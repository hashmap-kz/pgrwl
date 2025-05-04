//go:build debug

package logger

import (
	"context"
	"log/slog"
)

// Usage (go build -tags=debug):
//
// logger.DebugLazy(ctx, "processing msg", func() []slog.Attr {
// 	return []slog.Attr{
// 		slog.String("segment", expensiveSegment()),
// 		slog.Int("size", computeHeavyValue()),
// 	}
// })

func DebugLazy(ctx context.Context, msg string, buildAttrs func() []slog.Attr) {
	l := slog.Default()
	if l.Enabled(ctx, slog.LevelDebug) {
		l.LogAttrs(ctx, slog.LevelDebug, msg, buildAttrs()...)
	}
}
