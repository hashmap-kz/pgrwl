package logger

import (
	"context"
	"encoding"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeFormat   = "2006-01-02 15:04:05"
	defaultMessageWidth = 45
)

type PrettyTextOptions struct {
	Level slog.Leveler

	// AddSource adds slog.SourceKey attr, same idea as slog.HandlerOptions.AddSource.
	AddSource bool

	// ReplaceAttr is called to rewrite or drop attributes.
	//
	// To drop an attr, return slog.Attr{}.
	//
	// Built-in attrs passed to ReplaceAttr:
	//   - slog.TimeKey
	//   - slog.LevelKey
	//   - slog.SourceKey, when AddSource is true
	//
	// Message is not passed through ReplaceAttr because this handler prints it
	// as the fixed main log text.
	ReplaceAttr func(groups []string, attr slog.Attr) slog.Attr

	// TimeFormat controls timestamp rendering.
	// Default: "2006-01-02 15:04:05".
	TimeFormat string

	// MessageWidth controls the padded message column.
	// Default: 45.
	MessageWidth int
}

type PrettyTextHandler struct {
	out io.Writer

	opts *PrettyTextOptions

	mu *sync.Mutex

	attrs  []slog.Attr
	groups []string
}

func NewPrettyTextHandler(out io.Writer, opts *PrettyTextOptions) *PrettyTextHandler {
	if out == nil {
		out = io.Discard
	}

	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}

	return &PrettyTextHandler{
		out:  out,
		opts: opts,
		mu:   &sync.Mutex{},
	}
}

func (h *PrettyTextHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

//nolint:gocritic
func (h *PrettyTextHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder

	timeFormat := h.opts.TimeFormat
	if timeFormat == "" {
		timeFormat = defaultTimeFormat
	}

	messageWidth := h.opts.MessageWidth
	if messageWidth <= 0 {
		messageWidth = defaultMessageWidth
	}

	timeAttr := h.replace(nil, slog.Time(slog.TimeKey, r.Time))
	levelAttr := h.replace(nil, slog.Any(slog.LevelKey, r.Level))

	timestamp := formatTimeValue(timeAttr.Value, r.Time, timeFormat)
	level := formatLevelValue(levelAttr.Value, r.Level)

	_, _ = fmt.Fprintf(&b, "%-6s [%s] %-*s", level, timestamp, messageWidth, r.Message)

	if h.opts.AddSource && r.PC != 0 {
		sourceAttr := h.sourceAttr(r.PC)
		sourceAttr = h.replace(nil, sourceAttr)
		writeAttr(&b, nil, sourceAttr)
	}

	for _, a := range h.attrs {
		writeAttr(&b, h.groups, h.replace(h.groups, a))
	}

	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.groups, h.replace(h.groups, a))
		return true
	})

	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()

	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *PrettyTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *PrettyTextHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	next := *h
	next.groups = append(append([]string{}, h.groups...), name)
	return &next
}

func (h *PrettyTextHandler) replace(groups []string, a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()

	if a.Equal(slog.Attr{}) {
		return slog.Attr{}
	}

	if h.opts.ReplaceAttr == nil {
		return a
	}

	a = h.opts.ReplaceAttr(groups, a)
	a.Value = a.Value.Resolve()

	if a.Equal(slog.Attr{}) {
		return slog.Attr{}
	}

	return a
}

func (h *PrettyTextHandler) sourceAttr(pc uintptr) slog.Attr {
	frames := runtime.CallersFrames([]uintptr{pc})
	frame, _ := frames.Next()

	return slog.Any(slog.SourceKey, &slog.Source{
		Function: frame.Function,
		File:     frame.File,
		Line:     frame.Line,
	})
}

func writeAttr(b *strings.Builder, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()

	if a.Equal(slog.Attr{}) {
		return
	}

	if a.Value.Kind() == slog.KindGroup {
		groupName := a.Key

		nextGroups := groups
		if groupName != "" {
			nextGroups = append(append([]string{}, groups...), groupName)
		}

		for _, ga := range a.Value.Group() {
			writeAttr(b, nextGroups, ga)
		}

		return
	}

	key := a.Key
	if key == "" {
		return
	}

	if len(groups) > 0 {
		key = strings.Join(append(append([]string{}, groups...), key), ".")
	}

	b.WriteByte(' ')
	b.WriteString(formatAttrValue(key, a.Value))
}

func formatAttrValue(key string, value slog.Value) string {
	value = value.Resolve()

	switch value.Kind() {
	case slog.KindString:
		return fmt.Sprintf("%s=%s", key, quoteIfNeeded(value.String()))
	case slog.KindInt64:
		return fmt.Sprintf("%s=%d", key, value.Int64())
	case slog.KindUint64:
		return fmt.Sprintf("%s=%d", key, value.Uint64())
	case slog.KindFloat64:
		return fmt.Sprintf("%s=%f", key, value.Float64())
	case slog.KindBool:
		return fmt.Sprintf("%s=%t", key, value.Bool())
	case slog.KindDuration:
		return fmt.Sprintf("%s=%s", key, value.Duration())
	case slog.KindTime:
		return fmt.Sprintf("%s=%s", key, value.Time().Format(time.RFC3339))
	case slog.KindGroup:
		return fmt.Sprintf("%s=%q", key, value.Group())
	case slog.KindLogValuer:
		return formatAttrValue(key, value.Resolve())
	case slog.KindAny:
		return fmt.Sprintf("%s=%s", key, quoteIfNeeded(formatAnyValue(value)))
	default:
		return fmt.Sprintf("%s=%s", key, quoteIfNeeded(value.String()))
	}
}

func formatAnyValue(value slog.Value) (out string) {
	value = value.Resolve()

	defer func() {
		if r := recover(); r != nil {
			anyValue := value.Any()

			rv := reflect.ValueOf(anyValue)
			if rv.IsValid() && rv.Kind() == reflect.Pointer && rv.IsNil() {
				out = "<nil>"
				return
			}

			out = fmt.Sprintf("!PANIC: %v", r)
		}
	}()

	switch v := value.Any().(type) {
	case encoding.TextMarshaler:
		data, err := v.MarshalText()
		if err == nil {
			return string(data)
		}

		return fmt.Sprintf("%+v", v)

	case *slog.Source:
		return formatSource(v)

	default:
		return fmt.Sprintf("%+v", v)
	}
}

func formatSource(src *slog.Source) string {
	if src == nil {
		return ""
	}

	if src.File == "" {
		return src.Function
	}

	if src.Line <= 0 {
		return src.File
	}

	return fmt.Sprintf("%s:%d", src.File, src.Line)
}

func formatLevelValue(value slog.Value, fallback slog.Level) string {
	value = value.Resolve()

	switch value.Kind() {
	case slog.KindString:
		return strings.ToUpper(value.String())
	case slog.KindAny:
		if lvl, ok := value.Any().(slog.Level); ok {
			return normalizeLevel(lvl)
		}
		return strings.ToUpper(fmt.Sprint(value.Any()))
	default:
		return normalizeLevel(fallback)
	}
}

func normalizeLevel(level slog.Level) string {
	switch level {
	case slog.LevelDebug:
		return "DEBUG"
	case slog.LevelInfo:
		return "INFO"
	case slog.LevelWarn:
		return "WARN"
	case slog.LevelError:
		return "ERROR"
	default:
		return strings.ToUpper(level.String())
	}
}

func formatTimeValue(value slog.Value, fallback time.Time, layout string) string {
	value = value.Resolve()

	switch value.Kind() {
	case slog.KindTime:
		return value.Time().Format(layout)
	case slog.KindString:
		return value.String()
	default:
		return fallback.Format(layout)
	}
}

func quoteIfNeeded(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\r\"=") {
		return fmt.Sprintf("%q", s)
	}

	return s
}
