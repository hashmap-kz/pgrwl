package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	LevelTrace = slog.LevelDebug - 4
	LevelFatal = slog.LevelError + 4
)

func Init() {
	logLevel := os.Getenv("LOG_LEVEL")
	logFormat := os.Getenv("LOG_FORMAT")
	logAddSource := os.Getenv("LOG_ADD_SOURCE") != ""

	// Get logger level (INFO if not set)
	levels := map[string]slog.Level{
		"trace": LevelTrace,
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
		// print basename of a source. short-circuit: only when --log-add-source is enabled
		if logAddSource {
			if attr.Key == slog.SourceKey {
				if src, ok := attr.Value.Any().(*slog.Source); ok {
					// Trim to basename
					src.File = filepath.Base(src.File)
					attr.Value = slog.AnyValue(src)
				}
			}
		}
		// custom levels. short-circuit: replace only when the given --log-level not a slog one.
		if lvl <= slog.LevelDebug || lvl >= slog.LevelError {
			if attr.Key == slog.LevelKey {
				recLvl := attr.Value.Any().(slog.Level)
				switch recLvl {
				case LevelTrace:
					return slog.String(slog.LevelKey, "TRACE")
				case LevelFatal:
					return slog.String(slog.LevelKey, "FATAL")
				default:
					return attr // Keep default
				}
			}
		}
		return attr
	}

	// Create a base handler (TEXT if not set)
	var baseHandler slog.Handler
	if strings.EqualFold(logFormat, "json") {
		baseHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			AddSource:   logAddSource,
			Level:       lvl,
			ReplaceAttr: replaceAttr,
		})
	} else {
		baseHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			AddSource:   logAddSource,
			Level:       lvl,
			ReplaceAttr: replaceAttr,
		})
	}

	// Add global "pid" attribute to all logs
	logger := slog.New(baseHandler.WithAttrs([]slog.Attr{
		slog.Int("pid", os.Getpid()),
	}))

	// Set it as the default logger for the project
	slog.SetDefault(logger)
}
