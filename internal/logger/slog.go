package logger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func Init() {
	logLevel := os.Getenv("LOG_LEVEL")
	logFormat := os.Getenv("LOG_FORMAT")
	logAddSource := os.Getenv("LOG_ADD_SOURCE")

	// Get logger level (INFO if not set)
	levels := map[string]slog.Level{
		"trace": slog.LevelDebug,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	lvl := slog.LevelInfo
	if cfgLvl, ok := levels[strings.ToLower(logLevel)]; ok {
		lvl = cfgLvl
	}

	replaceAttr := func(_ []string, attr slog.Attr) slog.Attr {
		if attr.Key == slog.SourceKey {
			if src, ok := attr.Value.Any().(*slog.Source); ok {
				// Trim to basename
				src.File = filepath.Base(src.File)
				attr.Value = slog.AnyValue(src)
			}
		}
		return attr
	}

	// Create a base handler (TEXT if not set)
	var baseHandler slog.Handler
	if strings.EqualFold(logFormat, "json") {
		baseHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource:   logAddSource != "",
			Level:       lvl,
			ReplaceAttr: replaceAttr,
		})
	} else {
		//nolint:gocritic
		// baseHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		// 	AddSource:   logAddSource != "",
		// 	Level:       lvl,
		// 	ReplaceAttr: replaceAttr,
		// })
		baseHandler = &CleanHandler{
			Out:       os.Stderr,
			Level:     lvl,
			AddSource: logAddSource != "",
		}
	}

	// Add global "pid" attribute to all logs
	logger := slog.New(baseHandler.WithAttrs([]slog.Attr{
		slog.Int("pid", os.Getpid()),
	}))

	// Set it as the default logger for the project
	slog.SetDefault(logger)
}

// clean

type CleanHandler struct {
	Out       io.Writer
	Level     slog.Level
	AddSource bool
	attrs     []slog.Attr
}

func (h *CleanHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.Level
}

func (h *CleanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Append new attrs to existing ones
	newAttrs := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	newAttrs = append(newAttrs, h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &CleanHandler{
		Level:     h.Level,
		Out:       h.Out,
		AddSource: h.AddSource,
		attrs:     newAttrs,
	}
}

func (h *CleanHandler) WithGroup(_ string) slog.Handler {
	return h
}

//nolint:gocritic
func (h *CleanHandler) Handle(_ context.Context, r slog.Record) error {
	buf := bufPool.Get().(*bytes.Buffer) //nolint:errcheck
	buf.Reset()
	defer bufPool.Put(buf)

	ts := r.Time.Format("2006-01-02 15:04:05")
	level := strings.ToUpper(r.Level.String())

	// Base

	if h.AddSource {
		src := ""
		if r.PC != 0 {
			if fn := runtime.FuncForPC(r.PC); fn != nil {
				if file, line := fn.FileLine(r.PC - 1); file != "" {
					src = fmt.Sprintf("%s:%d", filepath.Base(file), line)
				}
			}
		}
		fmt.Fprintf(buf, "%s %-5s %-20s > %s", ts, level, src, r.Message)
	} else {
		fmt.Fprintf(buf, "%s %-5s > %s", ts, level, r.Message)
	}

	// Append attributes as key=value

	// Write handler-level attrs (from WithAttrs)
	for _, attr := range h.attrs {
		fmt.Fprintf(buf, " %s=%v", attr.Key, attr.Value.Any())
	}
	// Write record-level attrs
	r.Attrs(func(attr slog.Attr) bool {
		fmt.Fprintf(buf, " %s=%v", attr.Key, attr.Value.Any())
		return true
	})

	buf.WriteByte('\n')
	_, err := h.Out.Write(buf.Bytes())
	return err
}
