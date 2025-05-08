package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

type LoggingMiddleware struct {
	Logger  *slog.Logger
	Verbose bool
}

// responseWriter is a minimal wrapper for http.ResponseWriter that allows the
// written HTTP status code to be captured for logging.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
	rw.wroteHeader = true
}

// Middleware logs the incoming HTTP request & its duration.
func (m *LoggingMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.Verbose {
			start := time.Now()
			wrapped := wrapResponseWriter(w)
			next.ServeHTTP(wrapped, r)

			m.Logger.Debug("HTTP request",
				slog.Int("status", wrapped.status),
				slog.String("method", r.Method),
				slog.String("path", r.URL.EscapedPath()),
				slog.Duration("duration", time.Since(start)),
			)
		}
	})
}
