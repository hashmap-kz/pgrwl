package ui

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

const (
	stateStreaming = "streaming"
	stateStopped   = "stopped"
)

type View struct {
	Active        string
	Receivers     []Receiver
	SelectedIndex int
	Snapshot      Snapshot
	Query         string
	ExtFilter     string
	Page          int
	PageSize      int
	FilteredWAL   []WALFile
	VisibleWAL    []WALFile
	TotalPages    int
	PageNums      []int
	PageFrom      int
	PageTo        int
}

func parseTemplates() *template.Template {
	funcs := template.FuncMap{
		"active": func(got, want string) string {
			if got == want {
				return "active"
			}
			return ""
		},
		"chipVariant": chipVariant,
		"chipLabel":   chipLabel,
		"statusClass": statusClass,
		"statusText":  statusText,
		"dotColor":    dotColor,
		"ringColor":   ringColor,
		"walCount": func(v View) string {
			if len(v.Snapshot.WALFiles) == 0 {
				return ""
			}
			return fmt.Sprintf("%d", len(v.Snapshot.WALFiles))
		},
		"backupCount": func(v View) string {
			if len(v.Snapshot.Backups) == 0 {
				return ""
			}
			return fmt.Sprintf("%d", len(v.Snapshot.Backups))
		},
		"status": func(v View) *PgrwlStatus { return v.Snapshot.Status },
		"stream": func(v View) *StreamStatus {
			if v.Snapshot.Status == nil {
				return nil
			}
			return v.Snapshot.Status.StreamStatus
		},
		"config":         func(v View) *BriefConfig { return v.Snapshot.Config },
		"retentionLabel": retentionLabel,
		"retentionClass": retentionClass,
		"pageTitle": func(active string) string {
			switch active {
			case "status":
				return "pgrwl - Status"
			case "wal":
				return "pgrwl - WAL files"
			case "backups":
				return "pgrwl - Backups"
			case "config":
				return "pgrwl - Config"
			default:
				return "pgrwl"
			}
		},
		"yesNo": func(v bool) string {
			if v {
				return "yes"
			}
			return "no"
		},
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04")
		},
		"fmtShortTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Local().Format("15:04:05")
		},
		"fmtMB": func(v float64) string { return fmt.Sprintf("%.2f MB", v) },
		"fmtGB": func(v float64) string { return fmt.Sprintf("%.2f GB", v) },
		"extLabel": func(ext string) string {
			if ext == "" {
				return "plain"
			}
			return ext
		},
		"extVariant": func(ext string) string {
			switch ext {
			case "zst":
				return "blue"
			case "gz":
				//nolint:goconst
				return "amber"
			default:
				//nolint:goconst
				return "muted"
			}
		},
		"encLabel": func(v bool) string {
			if v {
				return "aes-gcm"
			}
			return "-"
		},
		"encVariant": func(v bool) string {
			if v {
				return "teal"
			}
			return "muted"
		},
		"backupVariant": backupVariant,
		"isFilter":      func(got, want string) bool { return got == want },
		"add":           func(a, b int) int { return a + b },
		"sub":           func(a, b int) int { return a - b },
		"url":           queryURL,
		"throughput": func(v View) []ThroughputBucket {
			return walThroughputLast24h(time.Now(), v.Snapshot.WALFiles)
		},
		"lastWAL":    lastWAL,
		"lastBackup": lastBackup,
		"duration":   duration,
		"restore":    restoreReadiness,
		"showFooter": func(v View) bool { return len(v.FilteredWAL) > 0 },
	}

	return template.Must(template.New("ui").Funcs(funcs).Parse(templates))
}

func chipVariant(s *PgrwlStatus) string {
	if s == nil || s.StreamStatus == nil {
		return "loading"
	}
	if s.StreamStatus.Running {
		return stateStreaming
	}
	return stateStopped
}

func chipLabel(s *PgrwlStatus) string {
	if s == nil || s.StreamStatus == nil {
		return "connecting"
	}
	if s.StreamStatus.Running {
		return stateStreaming
	}
	return stateStopped
}

func statusClass(s *PgrwlStatus) string {
	switch chipVariant(s) {
	case stateStreaming:
		return "ok"
	case stateStopped:
		//nolint:goconst
		return "bad"
	default:
		//nolint:goconst
		return "warn"
	}
}

func statusText(s *PgrwlStatus) string {
	if s == nil || s.StreamStatus == nil {
		return "UNKNOWN"
	}
	if s.StreamStatus.Running {
		return "STREAMING"
	}
	return "STOPPED"
}

func dotColor(s *PgrwlStatus) template.CSS {
	switch chipVariant(s) {
	case stateStreaming:
		return template.CSS("var(--green)")
	case stateStopped:
		return template.CSS("var(--red)")
	default:
		return template.CSS("var(--amber)")
	}
}

func ringColor(s *PgrwlStatus) template.CSS {
	if chipVariant(s) == stateStreaming {
		return template.CSS("var(--green)")
	}
	return template.CSS("var(--amber)")
}

func retentionLabel(c *BriefConfig) string {
	if c != nil && c.RetentionEnable {
		return "enabled"
	}
	return "disabled"
}

func retentionClass(c *BriefConfig) string {
	if c != nil && c.RetentionEnable {
		return "green"
	}
	return "amber"
}

func backupVariant(status string) string {
	switch strings.ToLower(status) {
	case "done", "ok", "success", "completed", "complete":
		return "green"
	case "running", "in_progress", "pending":
		return "amber"
	case "failed", "error":
		return "red"
	default:
		return "muted"
	}
}

// WAL stat basic calc

type ThroughputBucket struct {
	Label   string
	SizeMB  float64
	Count   int
	Height  int
	Current bool
}

func walThroughputLast24h(now time.Time, files []WALFile) []ThroughputBucket {
	now = now.Local()

	start := now.Add(-23 * time.Hour).Truncate(time.Hour)

	buckets := make([]ThroughputBucket, 24)
	for i := range buckets {
		t := start.Add(time.Duration(i) * time.Hour)
		buckets[i] = ThroughputBucket{
			Label:   t.Format("15:00"),
			Current: i == len(buckets)-1,
		}
	}

	for _, f := range files {
		if f.UploadedAt.IsZero() {
			continue
		}

		uploadedAt := f.UploadedAt.Local()

		if uploadedAt.Before(start) || uploadedAt.After(now) {
			continue
		}

		idx := int(uploadedAt.Truncate(time.Hour).Sub(start) / time.Hour)
		if idx < 0 || idx >= len(buckets) {
			continue
		}

		buckets[idx].SizeMB += f.SizeMB
		buckets[idx].Count++
	}

	var maxMB float64
	for _, b := range buckets {
		if b.SizeMB > maxMB {
			maxMB = b.SizeMB
		}
	}

	for i := range buckets {
		if maxMB == 0 {
			buckets[i].Height = 6
			continue
		}

		// Keep tiny activity visible, but cap at 100%.
		height := int((buckets[i].SizeMB / maxMB) * 100)
		if height < 6 {
			height = 6
		}
		if height > 100 {
			height = 100
		}

		buckets[i].Height = height
	}

	return buckets
}

func lastWAL(files []WALFile) *WALFile {
	if len(files) == 0 {
		return nil
	}
	latest := files[0]
	for _, f := range files[1:] {
		if f.UploadedAt.After(latest.UploadedAt) {
			latest = f
		}
	}
	return &latest
}

func lastBackup(backups []Backup) *Backup {
	if len(backups) == 0 {
		return nil
	}
	latest := backups[0]
	for _, b := range backups[1:] {
		if b.Started.After(latest.Started) {
			latest = b
		}
	}
	return &latest
}

func duration(started, finished time.Time) string {
	if started.IsZero() || finished.IsZero() || finished.Before(started) {
		return "-"
	}
	return finished.Sub(started).String()
}

type RestoreReadiness struct {
	PossibleLabel string
	StatusClass   string
	Summary       string

	BackupEndLSN  string
	CoveringWAL   string
	CoveringClass string

	SequenceLabel string
	SequenceClass string

	RestoreFrom string
	RestoreTo   string
	Note        string
}

//nolint:gocritic
func restoreReadiness(v View) RestoreReadiness {
	const unknown = "-"

	r := RestoreReadiness{
		PossibleLabel: "unknown",
		StatusClass:   "warn",
		Summary:       "not enough backup/WAL metadata",
		BackupEndLSN:  unknown,
		CoveringWAL:   unknown,
		CoveringClass: "warn",
		SequenceLabel: "unknown",
		SequenceClass: "warn",
		RestoreFrom:   unknown,
		RestoreTo:     unknown,
		Note:          "Restore readiness needs a completed backup and archived WAL files.",
	}

	backup := lastBackup(v.Snapshot.Backups)
	if backup == nil {
		r.PossibleLabel = "no"
		r.StatusClass = "bad"
		r.Summary = "no completed backup found"
		r.Note = "Create or discover a base backup before restore readiness can be checked."
		return r
	}

	r.RestoreFrom = fmtTimeLocal(backup.Started)
	r.BackupEndLSN = backup.WALStopLSN
	if r.BackupEndLSN == "" {
		r.BackupEndLSN = backup.WALStartLSN
	}
	if r.BackupEndLSN == "" {
		r.BackupEndLSN = unknown
	}

	if v.Snapshot.Status != nil && v.Snapshot.Status.StreamStatus != nil && v.Snapshot.Status.StreamStatus.LastFlushLSN != "" {
		r.RestoreTo = "now / " + v.Snapshot.Status.StreamStatus.LastFlushLSN
	} else if latest := lastWAL(v.Snapshot.WALFiles); latest != nil {
		r.RestoreTo = fmtTimeLocal(latest.UploadedAt)
	}

	coveringWAL, ok := walFileNameForLSN(r.BackupEndLSN, v.Snapshot.WALFiles)
	if !ok {
		if len(v.Snapshot.WALFiles) == 0 {
			r.PossibleLabel = "no"
			r.StatusClass = "bad"
			r.Summary = "no WAL files in archive"
			r.CoveringWAL = "missing"
			r.CoveringClass = "bad"
			r.SequenceLabel = "not checked"
			r.SequenceClass = "warn"
			r.Note = "No WAL files are present in the archive; cannot verify restore chain."
			return r
		}
		r.PossibleLabel = "no"
		r.StatusClass = "bad"
		r.Summary = "covering WAL not found"
		r.CoveringWAL = "missing"
		r.CoveringClass = "bad"
		r.SequenceLabel = "not checked"
		r.SequenceClass = "warn"
		r.Note = "The WAL file that covers the latest backup end LSN was not found in the archive."
		return r
	}

	r.CoveringWAL = coveringWAL
	r.CoveringClass = "ok"

	seq := walSequenceStatus(coveringWAL, v.Snapshot.WALFiles)
	if seq.ok {
		r.PossibleLabel = "yes"
		r.StatusClass = "ok"
		r.Summary = "chain is complete"
		r.SequenceLabel = "correct"
		r.SequenceClass = "ok"
		r.Note = fmt.Sprintf("WAL %s covers the latest backup end LSN. All later WAL files are present in order. No missing WAL detected.", shortWAL(coveringWAL))
		return r
	}

	r.PossibleLabel = "no"
	r.StatusClass = "bad"
	r.Summary = "WAL gap detected"
	r.SequenceLabel = "gap after " + shortWAL(seq.prev)
	r.SequenceClass = "bad"
	r.Note = fmt.Sprintf("WAL %s covers the backup end LSN, but the sequence is broken. First missing WAL: %s.", shortWAL(coveringWAL), shortWAL(seq.missing))

	return r
}

type walSequenceCheck struct {
	ok      bool
	prev    string
	missing string
}

func walSequenceStatus(coveringWAL string, files []WALFile) walSequenceCheck {
	coverSeg, ok := walSegmentNo(coveringWAL)
	if !ok {
		return walSequenceCheck{ok: false, prev: coveringWAL}
	}

	present := make(map[uint64]string, len(files))
	var maxSeg uint64
	for _, f := range files {
		seg, ok := walSegmentNo(f.Name)
		if !ok {
			seg, ok = walSegmentNo(f.Filename)
		}
		if !ok {
			continue
		}
		present[seg] = f.Name
		if seg > maxSeg {
			maxSeg = seg
		}
	}

	if _, ok := present[coverSeg]; !ok {
		return walSequenceCheck{ok: false, missing: coveringWAL}
	}

	prev := coveringWAL
	for seg := coverSeg + 1; seg <= maxSeg; seg++ {
		name, ok := present[seg]
		if !ok {
			return walSequenceCheck{ok: false, prev: prev, missing: walFileNameFromSeg(walTimeline(coveringWAL), seg)}
		}
		prev = name
	}

	return walSequenceCheck{ok: true}
}

func walFileNameForLSN(lsn string, files []WALFile) (string, bool) {
	if len(files) == 0 {
		return "", false
	}

	seg, ok := segmentNoFromLSN(lsn)
	if !ok {
		return "", false
	}

	timeline := uint64(1)
	if latest := lastWAL(files); latest != nil {
		if tl := walTimeline(latest.Name); tl != 0 {
			timeline = tl
		}
	}

	want := walFileNameFromSeg(timeline, seg)
	for _, f := range files {
		if strings.HasPrefix(f.Name, want) || strings.HasPrefix(f.Filename, want) {
			return want, true
		}
	}

	return want, false
}

func segmentNoFromLSN(lsn string) (uint64, bool) {
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0, false
	}

	hi, err := parseHex32(parts[0])
	if err != nil {
		return 0, false
	}
	lo, err := parseHex32(parts[1])
	if err != nil {
		return 0, false
	}

	const walSegmentSize = uint64(16 * 1024 * 1024)
	bytePos := (uint64(hi) << 32) | uint64(lo)
	return bytePos / walSegmentSize, true
}

func parseHex32(s string) (uint32, error) {
	var n uint32
	_, err := fmt.Sscanf(s, "%x", &n)
	return n, err
}

func walSegmentNo(name string) (uint64, bool) {
	base := walBaseName(name)
	if len(base) < 24 {
		return 0, false
	}
	log, err := parseHex32(base[8:16])
	if err != nil {
		return 0, false
	}
	seg, err := parseHex32(base[16:24])
	if err != nil {
		return 0, false
	}

	const walSegmentSize = uint64(16 * 1024 * 1024)
	segsPerXLogID := uint64(0x100000000) / walSegmentSize

	return uint64(log)*segsPerXLogID + uint64(seg), true
}

func walTimeline(name string) uint64 {
	base := walBaseName(name)
	if len(base) < 8 {
		return 0
	}
	tli, err := parseHex32(base[:8])
	if err != nil {
		return 0
	}
	return uint64(tli)
}

func walFileNameFromSeg(timeline, seg uint64) string {
	const walSegmentSize = uint64(16 * 1024 * 1024)
	segsPerXLogID := uint64(0x100000000) / walSegmentSize

	log := seg / segsPerXLogID
	walSeg := seg % segsPerXLogID

	return fmt.Sprintf("%08X%08X%08X", timeline, log, walSeg)
}

func walBaseName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 24 {
		return name[:24]
	}
	return name
}

func shortWAL(name string) string {
	base := walBaseName(name)
	if len(base) <= 4 {
		return base
	}
	return base[len(base)-4:]
}

func fmtTimeLocal(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

const templates = `
{{ define "layout" }}
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{ pageTitle .Active }}</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="/ui/static/app.css">
  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
</head>
<body>
  <div class="shell">
    {{ template "topbar" . }}
    <div class="body">
      {{ template "sidebar" . }}
      <main class="content">
        {{ if eq .Active "status" }}{{ template "status-page" . }}{{ end }}
        {{ if eq .Active "wal" }}{{ template "wal-page" . }}{{ end }}
        {{ if eq .Active "backups" }}{{ template "backups-page" . }}{{ end }}
        {{ if eq .Active "config" }}{{ template "config-page" . }}{{ end }}
      </main>
    </div>
  </div>
</body>
</html>
{{ end }}

{{ define "topbar" }}
<header class="topbar">
  <div class="brand">
    <span class="brand-title">pgrwl</span>
    <span class="brand-sub">WAL receiver</span>
  </div>

  <form class="recv-wrap" method="get" action="/ui/{{ .Active }}">
    <span class="recv-label">receiver</span>
    <select class="recv-select" name="receiver" onchange="this.form.submit()">
      {{ range $i, $r := .Receivers }}
        <option value="{{ $i }}" {{ if eq $.SelectedIndex $i }}selected{{ end }}>{{ $r.Label }} - {{ $r.Addr }}</option>
      {{ end }}
    </select>
    {{ if .Query }}<input type="hidden" name="q" value="{{ .Query }}">{{ end }}
    {{ if .ExtFilter }}<input type="hidden" name="ext" value="{{ .ExtFilter }}">{{ end }}
  </form>

  <div class="topbar-right">
    <span class="system-chip system-chip-{{ statusClass .Snapshot.Status }}">
      <span class="small-lamp {{ statusClass .Snapshot.Status }}"></span>{{ statusText .Snapshot.Status }}
    </span>
    <a class="btn" href="/ui/{{ .Active }}?receiver={{ .SelectedIndex }}">refresh</a>
  </div>
</header>
{{ end }}

{{ define "sidebar" }}
<nav class="sidebar">
  <div class="side-title">Navigation</div>
  <a class="nav-item {{ active .Active "status" }}" href="/ui/status?receiver={{ .SelectedIndex }}">
    <span>Status</span><span class="side-badge">live</span>
  </a>
  <a class="nav-item {{ active .Active "wal" }}" href="/ui/wal?receiver={{ .SelectedIndex }}">
    <span>WAL files</span>{{ if walCount . }}<span class="side-badge">{{ walCount . }}</span>{{ end }}
  </a>
  <a class="nav-item {{ active .Active "backups" }}" href="/ui/backups?receiver={{ .SelectedIndex }}">
    <span>Backups</span>{{ if backupCount . }}<span class="side-badge">{{ backupCount . }}</span>{{ end }}
  </a>
  <a class="nav-item {{ active .Active "config" }}" href="/ui/config?receiver={{ .SelectedIndex }}">
    <span>Config</span>
  </a>
</nav>
{{ end }}

{{ define "status-page" }}
<section id="status-panel" hx-get="/ui/fragments/status?receiver={{ .SelectedIndex }}" hx-trigger="every 15s" hx-swap="outerHTML">
  {{ template "status-panel-inner" . }}
</section>
{{ end }}

{{ define "status-panel" }}
<section id="status-panel" hx-get="/ui/fragments/status?receiver={{ .SelectedIndex }}" hx-trigger="every 15s" hx-swap="outerHTML">
  {{ template "status-panel-inner" . }}
</section>
{{ end }}

{{ define "status-panel-inner" }}
{{ if .Snapshot.Error }}<div class="error-banner"><span class="small-lamp bad"></span>{{ .Snapshot.Error }}</div>{{ end }}

{{ $ss := stream . }}
{{ $cfg := config . }}
{{ $lastWal := lastWAL .Snapshot.WALFiles }}
{{ $lastBackup := lastBackup .Snapshot.Backups }}

<div class="metrics-grid">
  <div class="metric">
    <div class="metric-label">Last Flush LSN</div>
    <div class="metric-value lsn">{{ if $ss }}{{ $ss.LastFlushLSN }}{{ else }}-{{ end }}</div>
    <div class="metric-meta">uptime: {{ if $ss }}{{ $ss.Uptime }}{{ else }}-{{ end }}</div>
  </div>
  <div class="metric">
    <div class="metric-label">WAL Archive</div>
    <div class="metric-value">{{ len .Snapshot.WALFiles }}</div>
    <div class="metric-meta">latest: {{ if $lastWal }}{{ $lastWal.Name }}{{ else }}-{{ end }}</div>
  </div>
  <div class="metric">
    <div class="metric-label">Retention</div>
    <div class="metric-value {{ retentionClass $cfg }}">{{ retentionLabel $cfg }}</div>
    <div class="metric-meta">backups: {{ len .Snapshot.Backups }}</div>
  </div>
</div>

{{ $restore := restore . }}
<section class="restore-readiness">
  <div class="module-header">
    <h2>Restore Readiness</h2>
    <span class="module-code">chain check</span>
  </div>

  <div class="restore-compact">
    <div class="restore-verdict">
      <div class="restore-label">restore possible</div>
      <div class="restore-value {{ $restore.StatusClass }}">{{ $restore.PossibleLabel }}</div>
      <div class="restore-sub">{{ $restore.Summary }}</div>
    </div>

    <div class="restore-facts">
      <div class="restore-fact">
        <span>backup end LSN</span>
        <strong>{{ $restore.BackupEndLSN }}</strong>
      </div>
      <div class="restore-fact">
        <span>covering WAL</span>
        <strong class="{{ $restore.CoveringClass }}">{{ $restore.CoveringWAL }}</strong>
      </div>
      <div class="restore-fact">
        <span>after covering WAL</span>
        <strong class="{{ $restore.SequenceClass }}">{{ $restore.SequenceLabel }}</strong>
      </div>
      <div class="restore-fact">
        <span>restore from</span>
        <strong>{{ $restore.RestoreFrom }}</strong>
      </div>
      <div class="restore-fact">
        <span>restore to</span>
        <strong>{{ $restore.RestoreTo }}</strong>
      </div>
    </div>

    <div class="restore-summary">
      <p>{{ $restore.Note }}</p>
    </div>
  </div>
</section>

<div class="dashboard-grid">
  <section class="module wide">
    <div class="module-header">
      <h2>WAL Upload Activity / Last 24h</h2>
      <span class="module-code">from uploaded WAL files</span>
    </div>

    <div class="module-body">
      {{ $throughput := throughput . }}

      <div class="bars">
        {{ range $b := $throughput }}
          <div
            class="bar {{ if $b.Current }}current{{ end }}"
            style="height: {{ $b.Height }}%"
            title="{{ $b.Label }} / {{ printf "%.1f" $b.SizeMB }} MB / {{ $b.Count }} WAL files">
          </div>
        {{ end }}
      </div>

      <div class="axis">
        {{ range $i, $b := $throughput }}
          {{ if or (eq $i 0) (eq $i 6) (eq $i 12) (eq $i 18) (eq $i 23) }}
            <span>{{ if $b.Current }}now{{ else }}{{ $b.Label }}{{ end }}</span>
          {{ end }}
        {{ end }}
      </div>
    </div>
  </section>

  <section class="module">
    <div class="module-header">
      <h2>Subsystem State</h2>
      <span class="module-code">live</span>
    </div>

    <div class="module-body status-grid">
      <div class="state-card">
        <div class="state-key">Receiver</div>
        <div class="state-value {{ statusClass .Snapshot.Status }}">
          {{ if $ss }}{{ if $ss.Running }}OK{{ else }}STOP{{ end }}{{ else }}WAIT{{ end }}
        </div>
      </div>

      <div class="state-card">
        <div class="state-key">Uploader</div>
        <div class="state-value ok">OK</div>
      </div>

      <div class="state-card">
        <div class="state-key">Retention</div>
        <div class="state-value {{ retentionClass $cfg }}">
          {{ if $cfg }}{{ if $cfg.RetentionEnable }}ON{{ else }}OFF{{ end }}{{ else }}OFF{{ end }}
        </div>
      </div>

      <div class="state-card">
        <div class="state-key">Backup</div>
        <div class="state-value {{ if $lastBackup }}{{ backupVariant $lastBackup.Status }}{{ else }}warn{{ end }}">
          {{ if $lastBackup }}{{ $lastBackup.Status }}{{ else }}NONE{{ end }}
        </div>
      </div>
    </div>
  </section>
</div>

<section class="module">
  <div class="module-header"><h2>Recent Operations</h2><span class="module-code">operator log</span></div>
  <div class="module-body no-pad">
    <table class="table">
      <thead><tr><th>event</th><th>time</th><th>state</th><th>notes</th></tr></thead>
      <tbody>
        {{ if $lastWal }}
        <tr><td class="mono">WAL uploaded</td><td class="mono muted">{{ fmtShortTime $lastWal.UploadedAt }}</td><td><span class="badge badge-ok">ok</span></td><td class="mono muted">{{ $lastWal.Name }} · {{ extLabel $lastWal.Ext }}{{ if $lastWal.Encrypted }} · encrypted{{ end }}</td></tr>
        {{ end }}
        {{ if $lastBackup }}
        <tr><td class="mono">backup completed</td><td class="mono muted">{{ fmtShortTime $lastBackup.Finished }}</td><td><span class="badge badge-{{ backupVariant $lastBackup.Status }}">{{ $lastBackup.Status }}</span></td><td class="mono muted">{{ $lastBackup.Label }} · {{ fmtGB $lastBackup.SizeGB }} · {{ duration $lastBackup.Started $lastBackup.Finished }}</td></tr>
        {{ end }}
        <tr><td class="mono">receiver heartbeat</td><td class="mono muted">now</td><td><span class="badge badge-{{ statusClass .Snapshot.Status }}">{{ chipLabel .Snapshot.Status }}</span></td><td class="mono muted">slot: {{ if $ss }}{{ $ss.Slot }}{{ else }}-{{ end }}</td></tr>
      </tbody>
    </table>
  </div>
</section>
{{ end }}

{{ define "wal-page" }}
<div class="filter-bar">
  <form hx-get="/ui/fragments/wal-table" hx-target="#wal-table" hx-swap="outerHTML" class="filter-form">
    <input type="hidden" name="receiver" value="{{ .SelectedIndex }}">
    <input class="search" type="text" name="q" value="{{ .Query }}" placeholder="filter by filename" hx-trigger="keyup changed delay:250ms" hx-get="/ui/fragments/wal-table" hx-target="#wal-table" hx-include="closest form">
    <button class="filter-chip {{ if isFilter .ExtFilter "all" }}active{{ end }}" name="ext" value="all">all</button>
    <button class="filter-chip {{ if isFilter .ExtFilter "zst" }}active{{ end }}" name="ext" value="zst">zst</button>
    <button class="filter-chip {{ if isFilter .ExtFilter "gz" }}active{{ end }}" name="ext" value="gz">gz</button>
    <button class="filter-chip {{ if isFilter .ExtFilter "plain" }}active{{ end }}" name="ext" value="plain">plain</button>
  </form>
</div>
{{ template "wal-table" . }}
{{ end }}

{{ define "wal-table" }}
<div class="module" id="wal-table">
  <div class="module-header"><h2>WAL Archive</h2><span class="module-code">{{ len .FilteredWAL }} files</span></div>
  <div class="module-body no-pad">
    <table class="table">
      <thead><tr><th>filename</th><th>size</th><th>uploaded</th><th>compression</th><th>encryption</th></tr></thead>
      <tbody>
        {{ if eq (len .VisibleWAL) 0 }}<tr><td colspan="5"><div class="empty">no files match filter</div></td></tr>{{ end }}
        {{ range .VisibleWAL }}
        <tr>
          <td class="mono">{{ .Name }}{{ if .Ext }}<span class="muted">.{{ .Ext }}</span>{{ end }}</td>
          <td class="mono">{{ fmtMB .SizeMB }}</td>
          <td class="mono muted">{{ fmtTime .UploadedAt }}</td>
          <td><span class="badge badge-{{ extVariant .Ext }}">{{ extLabel .Ext }}</span></td>
          <td><span class="badge badge-{{ encVariant .Encrypted }}">{{ encLabel .Encrypted }}</span></td>
        </tr>
        {{ end }}
      </tbody>
    </table>
  </div>
  {{ if showFooter . }}
  <div class="footer">
    <span class="footer-info">showing {{ .PageFrom }}-{{ .PageTo }} of {{ len .FilteredWAL }}</span>
    <div class="pagination">
      <a class="pg-btn {{ if eq .Page 0 }}disabled{{ end }}" hx-get="{{ url "/ui/fragments/wal-table" .SelectedIndex .Query .ExtFilter 0 }}" hx-target="#wal-table" hx-swap="outerHTML">first</a>
      <a class="pg-btn {{ if eq .Page 0 }}disabled{{ end }}" hx-get="{{ url "/ui/fragments/wal-table" .SelectedIndex .Query .ExtFilter (sub .Page 1) }}" hx-target="#wal-table" hx-swap="outerHTML">prev</a>
      {{ range .PageNums }}<a class="pg-btn {{ if eq $.Page . }}pg-active{{ end }}" hx-get="{{ url "/ui/fragments/wal-table" $.SelectedIndex $.Query $.ExtFilter . }}" hx-target="#wal-table" hx-swap="outerHTML">{{ add . 1 }}</a>{{ end }}
      <a class="pg-btn {{ if ge .Page (sub .TotalPages 1) }}disabled{{ end }}" hx-get="{{ url "/ui/fragments/wal-table" .SelectedIndex .Query .ExtFilter (add .Page 1) }}" hx-target="#wal-table" hx-swap="outerHTML">next</a>
      <a class="pg-btn {{ if ge .Page (sub .TotalPages 1) }}disabled{{ end }}" hx-get="{{ url "/ui/fragments/wal-table" .SelectedIndex .Query .ExtFilter (sub .TotalPages 1) }}" hx-target="#wal-table" hx-swap="outerHTML">last</a>
    </div>
  </div>
  {{ end }}
</div>
{{ end }}

{{ define "backups-page" }}
{{ template "backups-table" . }}
{{ end }}

{{ define "backups-table" }}
<div class="module" id="backups-table" hx-get="/ui/fragments/backups-table?receiver={{ .SelectedIndex }}" hx-trigger="every 15s" hx-swap="outerHTML">
  <div class="module-header"><h2>Backup Registry</h2><span class="module-code">{{ len .Snapshot.Backups }} backups</span></div>
  <div class="module-body no-pad">
    <table class="table">
      <thead><tr><th>label</th><th>started</th><th>duration</th><th>size</th><th>start lsn</th><th>end lsn</th><th>status</th></tr></thead>
      <tbody>
        {{ if eq (len .Snapshot.Backups) 0 }}<tr><td colspan="7"><div class="empty">no backups found</div></td></tr>{{ end }}
        {{ range .Snapshot.Backups }}
        <tr>
          <td class="mono">{{ .Label }}</td>
          <td class="mono muted">{{ fmtTime .Started }}</td>
          <td class="mono">{{ duration .Started .Finished }}</td>
          <td class="mono">{{ fmtGB .SizeGB }}</td>
          <td class="mono lsn">{{ .WALStartLSN }}</td>
          <td class="mono lsn">{{ .WALStopLSN }}</td>
          <td><span class="badge badge-{{ backupVariant .Status }}">{{ .Status }}</span></td>
        </tr>
        {{ end }}
      </tbody>
    </table>
  </div>
</div>
{{ end }}

{{ define "config-page" }}
{{ $ss := stream . }}{{ $cfg := config . }}
<div class="page-head">
  <div>
    <div class="eyebrow">system module</div>
    <h1>Configuration</h1>
  </div>
  <div class="page-actions">
    <span class="system-chip"><span class="small-lamp {{ if .Snapshot.Error }}bad{{ else }}ok{{ end }}"></span>{{ if .Snapshot.Error }}unreachable{{ else }}receiver ok{{ end }}</span>
  </div>
</div>

<div class="config-grid">
  <section class="module">
    <div class="module-header"><h2>Receiver</h2><span class="module-code">target</span></div>
    <div class="kv-list">
      <div class="kv-row"><span>label</span><strong>{{ .Snapshot.Receiver.Label }}</strong></div>
      <div class="kv-row"><span>address</span><strong>{{ .Snapshot.Receiver.Addr }}</strong></div>
      <div class="kv-row"><span>health</span><strong><span class="badge badge-{{ if .Snapshot.Error }}red{{ else }}green{{ end }}">{{ if .Snapshot.Error }}unreachable{{ else }}ok{{ end }}</span></strong></div>
    </div>
  </section>

  <section class="module">
    <div class="module-header"><h2>Stream</h2><span class="module-code">replication</span></div>
    <div class="kv-list">
      <div class="kv-row"><span>slot</span><strong>{{ if $ss }}{{ $ss.Slot }}{{ else }}-{{ end }}</strong></div>
      <div class="kv-row"><span>timeline</span><strong>{{ if $ss }}{{ $ss.Timeline }}{{ else }}-{{ end }}</strong></div>
      <div class="kv-row"><span>running mode</span><strong>{{ if .Snapshot.Status }}<span class="badge badge-blue">{{ .Snapshot.Status.RunningMode }}</span>{{ else }}-{{ end }}</strong></div>
      <div class="kv-row"><span>uptime</span><strong>{{ if $ss }}{{ $ss.Uptime }}{{ else }}-{{ end }}</strong></div>
    </div>
  </section>

  <section class="module">
    <div class="module-header"><h2>API Endpoints</h2><span class="module-code">contract</span></div>
    <div class="kv-list">
      <div class="kv-row"><span>GET /api/v1/status</span><strong><span class="badge badge-green">live</span></strong></div>
      <div class="kv-row"><span>GET /api/v1/brief-config</span><strong><span class="badge badge-green">live</span></strong></div>
      <div class="kv-row"><span>GET /api/v1/wals</span><strong><span class="badge badge-amber">optional</span></strong></div>
      <div class="kv-row"><span>GET /api/v1/backups</span><strong><span class="badge badge-amber">optional</span></strong></div>
      <div class="kv-row"><span>GET /healthz</span><strong><span class="badge badge-blue">health</span></strong></div>
    </div>
  </section>

  <section class="module">
    <div class="module-header"><h2>UI Runtime</h2><span class="module-code">local</span></div>
    <div class="kv-list">
      <div class="kv-row"><span>ui version</span><strong>0.1.0</strong></div>
      <div class="kv-row"><span>auto-refresh</span><strong>15s</strong></div>
      <div class="kv-row"><span>receivers configured</span><strong>{{ len .Receivers }}</strong></div>
      <div class="kv-row"><span>retention</span><strong class="{{ retentionClass $cfg }}">{{ retentionLabel $cfg }}</strong></div>
    </div>
  </section>
</div>
{{ end }}
`

//nolint:unused
func _keepStringsPackageUsed() { _ = strings.Builder{} }
