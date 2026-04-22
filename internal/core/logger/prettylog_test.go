package logger

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrettyTextHandlerBasicFormatting(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("application started")

	got := buf.String()

	assert.Regexp(t,
		regexp.MustCompile(`^INFO\s+\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] application started\s+\n$`),
		got,
	)
}

func TestPrettyTextHandlerFormatsAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("streaming WAL",
		slog.String("component", "receiver"),
		slog.String("slot", "pgrwl"),
		slog.Int("timeline", 1),
		slog.Bool("active", true),
		slog.Duration("lag", 1500*time.Millisecond),
	)

	got := buf.String()

	assert.Contains(t, got, "INFO")
	assert.Contains(t, got, "streaming WAL")
	assert.Contains(t, got, "component=receiver")
	assert.Contains(t, got, "slot=pgrwl")
	assert.Contains(t, got, "timeline=1")
	assert.Contains(t, got, "active=true")
	assert.Contains(t, got, "lag=1.5s")
}

func TestPrettyTextHandlerQuotesStringsWhenNeeded(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("quoted attrs",
		slog.String("empty", ""),
		slog.String("space", "hello world"),
		slog.String("equals", "a=b"),
		slog.String("quote", `hello "world"`),
		slog.String("plain", "hello"),
	)

	got := buf.String()

	assert.Contains(t, got, `empty=""`)
	assert.Contains(t, got, `space="hello world"`)
	assert.Contains(t, got, `equals="a=b"`)
	assert.Contains(t, got, `quote="hello \"world\""`)
	assert.Contains(t, got, `plain=hello`)
}

func TestPrettyTextHandlerDebugDisabledByDefault(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{}))

	log.Debug("hidden")
	log.Info("visible")

	got := buf.String()

	assert.NotContains(t, got, "hidden")
	assert.Contains(t, got, "visible")
}

func TestPrettyTextHandlerDebugEnabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Debug("debug message")

	got := buf.String()

	assert.Contains(t, got, "DEBUG")
	assert.Contains(t, got, "debug message")
}

func TestPrettyTextHandlerCustomNegativeLevel(t *testing.T) {
	t.Parallel()

	const LevelTrace slog.Level = -8

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: LevelTrace,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.LevelKey {
				if lvl, ok := attr.Value.Any().(slog.Level); ok && lvl == LevelTrace {
					return slog.String(slog.LevelKey, "TRACE")
				}
			}

			return attr
		},
	}))

	//nolint:staticcheck
	log.LogAttrs(nil, LevelTrace, "trace message",
		slog.String("component", "receiver"),
	)

	got := buf.String()

	assert.Contains(t, got, "TRACE")
	assert.Contains(t, got, "trace message")
	assert.Contains(t, got, "component=receiver")
}

func TestPrettyTextHandlerReplaceAttrCanRewriteAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == "password" {
				return slog.String(attr.Key, "***")
			}

			if len(groups) == 1 && groups[0] == "pg" && attr.Key == "slot" {
				return slog.String("replication_slot", attr.Value.String())
			}

			return attr
		},
	}))

	log.WithGroup("pg").Info("connect",
		slog.String("slot", "pgrwl"),
		slog.String("password", "secret"),
	)

	got := buf.String()

	assert.Contains(t, got, "pg.replication_slot=pgrwl")
	assert.Contains(t, got, "pg.password=***")
	assert.NotContains(t, got, "secret")
	assert.NotContains(t, got, "pg.slot=pgrwl")
}

func TestPrettyTextHandlerReplaceAttrCanDropAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "token" {
				return slog.Attr{}
			}

			return attr
		},
	}))

	log.Info("auth",
		slog.String("component", "api"),
		slog.String("token", "secret-token"),
	)

	got := buf.String()

	assert.Contains(t, got, "component=api")
	assert.NotContains(t, got, "token=")
	assert.NotContains(t, got, "secret-token")
}

func TestPrettyTextHandlerReplaceAttrCanRewriteTime(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, "CUSTOM-TIME")
			}

			return attr
		},
	}))

	log.Info("time rewritten")

	got := buf.String()

	assert.Contains(t, got, "INFO")
	assert.Contains(t, got, "[CUSTOM-TIME]")
	assert.Contains(t, got, "time rewritten")
}

func TestPrettyTextHandlerCustomTimeFormatAndMessageWidth(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level:        slog.LevelDebug,
		TimeFormat:   time.RFC3339,
		MessageWidth: 10,
	}))

	log.Info("msg", slog.String("component", "test"))

	got := buf.String()

	assert.Regexp(t,
		regexp.MustCompile(`^INFO\s+\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.*\] msg\s+component=test\n$`),
		got,
	)
}

func TestPrettyTextHandlerWithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	})).With(
		slog.String("service", "pgrwl"),
		slog.String("component", "receiver"),
	)

	log.Info("streaming WAL",
		slog.String("slot", "pgrwl"),
	)

	got := buf.String()

	assert.Contains(t, got, "service=pgrwl")
	assert.Contains(t, got, "component=receiver")
	assert.Contains(t, got, "slot=pgrwl")
}

func TestPrettyTextHandlerWithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	})).
		WithGroup("pg").
		With(
			slog.String("slot", "pgrwl"),
		)

	log.Info("streaming WAL",
		slog.Int("timeline", 1),
	)

	got := buf.String()

	assert.Contains(t, got, "pg.slot=pgrwl")
	assert.Contains(t, got, "pg.timeline=1")
}

func TestPrettyTextHandlerNestedGroups(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	})).
		WithGroup("pg").
		WithGroup("wal")

	log.Info("uploading",
		slog.String("file", "00000001000000000000000A"),
	)

	got := buf.String()

	assert.Contains(t, got, "pg.wal.file=00000001000000000000000A")
}

func TestPrettyTextHandlerSlogGroupAttr(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("group attr",
		slog.Group("wal",
			slog.String("file", "00000001000000000000000A"),
			slog.Int("timeline", 1),
		),
	)

	got := buf.String()

	assert.Contains(t, got, "wal.file=00000001000000000000000A")
	assert.Contains(t, got, "wal.timeline=1")
}

func TestPrettyTextHandlerAddSource(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.SourceKey {
				if src, ok := attr.Value.Any().(*slog.Source); ok {
					cp := *src
					cp.File = filepath.Base(cp.File)
					attr.Value = slog.AnyValue(&cp)
				}
			}

			return attr
		},
	}))

	log.Info("with source")

	got := buf.String()

	assert.Contains(t, got, "with source")
	assert.Regexp(t, regexp.MustCompile(`source=.+_test\.go:\d+`), got)
	assert.NotContains(t, got, `source="&{`)
}

func TestFormatSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  *slog.Source
		want string
	}{
		{
			name: "nil",
			src:  nil,
			want: "",
		},
		{
			name: "function only",
			src: &slog.Source{
				Function: "main.main",
			},
			want: "main.main",
		},
		{
			name: "file without line",
			src: &slog.Source{
				File: "main.go",
			},
			want: "main.go",
		},
		{
			name: "file with line",
			src: &slog.Source{
				File: "main.go",
				Line: 42,
			},
			want: "main.go:42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, formatSource(tt.src))
		})
	}
}

type goodTextMarshaler struct {
	value string
}

func (m goodTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(m.value), nil
}

type failingTextMarshaler struct{}

func (failingTextMarshaler) MarshalText() ([]byte, error) {
	return nil, errors.New("marshal failed")
}

type panicTextMarshaler struct{}

func (*panicTextMarshaler) MarshalText() ([]byte, error) {
	panic("boom")
}

func TestPrettyTextHandlerAnyTextMarshaler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("text marshaler",
		slog.Any("value", goodTextMarshaler{value: "hello"}),
	)

	got := buf.String()

	assert.Contains(t, got, "value=hello")
}

func TestPrettyTextHandlerAnyTextMarshalerErrorFallsBack(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	log.Info("text marshaler error",
		slog.Any("value", failingTextMarshaler{}),
	)

	got := buf.String()

	assert.Contains(t, got, "text marshaler error")
	assert.Contains(t, got, "value={}")
}

func TestPrettyTextHandlerAnyTextMarshalerNilPointerPanicBecomesNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	var bad *panicTextMarshaler

	require.NotPanics(t, func() {
		log.Info("panic nil marshaler",
			slog.Any("bad", bad),
		)
	})

	got := buf.String()

	assert.Contains(t, got, "panic nil marshaler")
	assert.Contains(t, got, "bad=<nil>")
}

func TestPrettyTextHandlerAnyTextMarshalerNonNilPanicBecomesPanicMarker(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	require.NotPanics(t, func() {
		log.Info("panic marshaler",
			slog.Any("bad", &panicTextMarshaler{}),
		)
	})

	got := buf.String()

	assert.Contains(t, got, "panic marshaler")
	assert.Contains(t, got, `bad="!PANIC: boom"`)
}

func TestPrettyTextHandlerNilOutputDoesNotPanic(t *testing.T) {
	t.Parallel()

	log := slog.New(NewPrettyTextHandler(nil, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	require.NotPanics(t, func() {
		log.Info("discarded")
	})
}

func TestPrettyTextHandlerConcurrentLogging(t *testing.T) {
	var buf bytes.Buffer

	log := slog.New(NewPrettyTextHandler(&buf, &PrettyTextOptions{
		Level: slog.LevelDebug,
	}))

	const goroutines = 32
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		g := g

		go func() {
			defer wg.Done()

			for i := 0; i < perGoroutine; i++ {
				log.Info("concurrent log",
					slog.Int("goroutine", g),
					slog.Int("iteration", i),
				)
			}
		}()
	}

	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")

	require.Len(t, lines, goroutines*perGoroutine)

	for _, line := range lines {
		assert.Contains(t, line, "INFO")
		assert.Contains(t, line, "concurrent log")
		assert.Contains(t, line, "goroutine=")
		assert.Contains(t, line, "iteration=")
	}
}

func TestPrettyTextHandlerWriteErrorReturnedFromHandle(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")

	handler := NewPrettyTextHandler(errWriter{err: wantErr}, &PrettyTextOptions{
		Level: slog.LevelDebug,
	})

	record := slog.NewRecord(time.Now(), slog.LevelInfo, "message", 0)

	//nolint:staticcheck
	err := handler.Handle(nil, record)

	assert.ErrorIs(t, err, wantErr)
}

type errWriter struct {
	err error
}

func (w errWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestFormatAttrValueKinds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 20, 12, 7, 12, 0, time.UTC)

	tests := []struct {
		name string
		attr slog.Attr
		want string
	}{
		{
			name: "string",
			attr: slog.String("k", "v"),
			want: "k=v",
		},
		{
			name: "int",
			attr: slog.Int("k", 42),
			want: "k=42",
		},
		{
			name: "uint64",
			attr: slog.Uint64("k", 42),
			want: "k=42",
		},
		{
			name: "float64",
			attr: slog.Float64("k", 1.25),
			want: "k=1.250000",
		},
		{
			name: "bool",
			attr: slog.Bool("k", true),
			want: "k=true",
		},
		{
			name: "duration",
			attr: slog.Duration("k", 2*time.Second),
			want: "k=2s",
		},
		{
			name: "time",
			attr: slog.Time("k", now),
			want: "k=2026-04-20T12:07:12Z",
		},
		{
			name: "any stringer-ish",
			attr: slog.Any("k", fmt.Stringer(nil)),
			want: "k=<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, formatAttrValue(tt.attr.Key, tt.attr.Value))
		})
	}
}
