//go:build debug

package logger

import (
	"context"
	"log/slog"
)

// Usage:
//
// mylog.Debug(ctx, "processing msg", func() []slog.Attr {
// 	return []slog.Attr{
// 		slog.String("segment", expensiveSegment()),
// 		slog.Int("size", computeHeavyValue()),
// 	}
// })

func DebugLazy(ctx context.Context, msg string, build func() []slog.Attr) {
	l := slog.Default()
	if l.Enabled(ctx, slog.LevelDebug) {
		l.LogAttrs(ctx, slog.LevelDebug, msg, build()...)
	}
}
