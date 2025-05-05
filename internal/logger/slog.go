package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func Init() {
	logLevel := os.Getenv("LOG_LEVEL")
	logFormat := os.Getenv("LOG_FORMAT")
	logAddSource := os.Getenv("LOG_ADD_SOURCE") != ""

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
		if logAddSource {
			if attr.Key == slog.SourceKey {
				if src, ok := attr.Value.Any().(*slog.Source); ok {
					// Trim to basename
					src.File = filepath.Base(src.File)
					attr.Value = slog.AnyValue(src)
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
