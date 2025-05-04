//go:build !debug

package logger

import (
	"context"
	"log/slog"
)

// These are included in release builds only
// Debug logs are fully eliminated (including attr evaluation)

func DebugLazy(_ context.Context, _ string, _ func() []slog.Attr) {
	// no-op
}
