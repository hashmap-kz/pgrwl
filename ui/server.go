package ui

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

const pageSize = 15

type Options struct {
	Logger    *slog.Logger
	Receivers []Receiver
	Client    Client
}

type Server struct {
	log       *slog.Logger
	receivers []Receiver
	client    Client
	tpl       *template.Template
}

func NewServer(opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	receivers := opts.Receivers
	if len(receivers) == 0 {
		receivers = []Receiver{{Label: "localhost", Addr: "http://127.0.0.1:7070"}}
	}

	client := opts.Client
	if client == nil {
		client = NewHTTPClient()
	}

	return &Server{
		log:       log,
		receivers: receivers,
		client:    client,
		tpl:       parseTemplates(),
	}
}

func (s *Server) Mount(mux *http.ServeMux) {
	staticRoot, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Errorf("create static filesystem: %w", err))
	}

	mux.Handle(
		"/ui/static/",
		http.StripPrefix("/ui/static/", http.FileServer(http.FS(staticRoot))),
	)
	mux.HandleFunc("/ui", s.redirectRoot)
	mux.HandleFunc("/ui/", s.page)
	mux.HandleFunc("/ui/fragments/status", s.statusFragment)
	mux.HandleFunc("/ui/fragments/wal-table", s.walTableFragment)
	mux.HandleFunc("/ui/fragments/backups-table", s.backupsTableFragment)
}

func (s *Server) redirectRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/ui/status", http.StatusSeeOther)
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/ui/")
	switch name {
	case "status", "wal", "backups", "config":
		// ok
	default:
		http.Redirect(w, r, "/ui/status", http.StatusSeeOther)
		return
	}

	view := s.buildView(r, name)
	s.render(w, r, "layout", view)
}

func (s *Server) statusFragment(w http.ResponseWriter, r *http.Request) {
	view := s.buildView(r, "status")
	s.render(w, r, "status-panel", view)
}

func (s *Server) walTableFragment(w http.ResponseWriter, r *http.Request) {
	view := s.buildView(r, "wal")
	s.render(w, r, "wal-table", view)
}

func (s *Server) backupsTableFragment(w http.ResponseWriter, r *http.Request) {
	view := s.buildView(r, "backups")
	s.render(w, r, "backups-table", view)
}

func (s *Server) buildView(r *http.Request, active string) View {
	selected := selectedReceiverIndex(r, len(s.receivers))
	snap := s.client.Snapshot(r.Context(), s.receivers[selected])

	walFilter := r.URL.Query().Get("ext")
	if walFilter == "" {
		walFilter = "all"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	page := parseNonNegativeInt(r.URL.Query().Get("page"))

	walRows := filterWAL(snap.WALFiles, query, walFilter)
	totalPages := max(1, (len(walRows)+pageSize-1)/pageSize)
	if page >= totalPages {
		page = totalPages - 1
	}
	from := page * pageSize
	to := min(from+pageSize, len(walRows))
	if from > to {
		from = to
	}

	return View{
		Active:        active,
		Receivers:     s.receivers,
		SelectedIndex: selected,
		Snapshot:      snap,
		Query:         query,
		ExtFilter:     walFilter,
		Page:          page,
		PageSize:      pageSize,
		FilteredWAL:   walRows,
		VisibleWAL:    walRows[from:to],
		TotalPages:    totalPages,
		PageNums:      pageNums(page, totalPages),
		PageFrom:      displayFrom(from, len(walRows)),
		PageTo:        to,
	}
}

func (s *Server) render(w http.ResponseWriter, _ *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render failed", slog.String("template", name), slog.Any("err", err))
	}
}

func selectedReceiverIndex(r *http.Request, count int) int {
	idx := parseNonNegativeInt(r.URL.Query().Get("receiver"))
	if idx >= count {
		return 0
	}
	return idx
}

func parseNonNegativeInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func filterWAL(files []WALFile, query, ext string) []WALFile {
	query = strings.ToLower(query)
	out := make([]WALFile, 0, len(files))
	for _, f := range files {
		if query != "" && !strings.Contains(strings.ToLower(f.Filename), query) {
			continue
		}
		switch ext {
		case "zst", "gz":
			if f.Ext != ext {
				continue
			}
		case "plain":
			if f.Ext != "" {
				continue
			}
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UploadedAt.After(out[j].UploadedAt) })
	return out
}

func pageNums(page, total int) []int {
	start := max(0, page-2)
	end := min(total, start+5)
	start = max(0, end-5)
	pages := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		pages = append(pages, i)
	}
	return pages
}

func displayFrom(from, total int) int {
	if total == 0 {
		return 0
	}
	return from + 1
}

func queryURL(path string, receiver int, q, ext string, page int) string {
	return fmt.Sprintf("%s?receiver=%d&q=%s&ext=%s&page=%d", path, receiver, template.URLQueryEscaper(q), template.URLQueryEscaper(ext), page)
}
