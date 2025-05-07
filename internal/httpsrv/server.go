package httpsrv

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var (
	expectedToken = os.Getenv("PGRWL_AUTH_TOKEN")
	limiter       = rate.NewLimiter(5, 10) // 5 req/sec, burst 10
)

// ---- Server ----

// ---- Struct ----

type HTTPServer struct {
	srv    *http.Server
	logger *slog.Logger
}

// ---- Constructor ----

func NewHTTPServer(_ context.Context, addr string) *HTTPServer {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Build middleware chain
	secureChain := MiddlewareChain(
		safeHandlerMiddleware,
		loggingMiddleware,
		rateLimitMiddleware,
		tokenAuthMiddleware,
	)

	mux.Handle("/status", secureChain(http.HandlerFunc(statusHandler)))
	mux.Handle("POST /retention", secureChain(http.HandlerFunc(walRetentionHandler)))

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	return &HTTPServer{
		srv:    srv,
		logger: slog.With("component", "http-server"),
	}
}

func (h *HTTPServer) Start(_ context.Context) {
	go func() {
		h.logger.Info("HTTP server listening", slog.String("addr", h.srv.Addr))
		if err := h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("HTTP server error", slog.Any("err", err))
		}
	}()
}

func (h *HTTPServer) Shutdown(ctx context.Context) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	h.logger.Info("shutting down HTTP server")
	if err := h.srv.Shutdown(timeoutCtx); err != nil {
		h.logger.Error("error during HTTP server shutdown", slog.Any("err", err))
	} else {
		h.logger.Info("HTTP server shut down cleanly")
	}
}

// ---- Handler ----

func safeHandlerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", slog.Any("err", rec))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func statusHandler(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{
		"status": "UP",
	})
}

// ---- Middlewares ----

func rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenAuthMiddleware primitive authorization (for future use with any IPD providers)
func tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			WriteJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "missing or incorrect token",
			})
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if expectedToken == "" || token != expectedToken {
			WriteJSON(w, http.StatusForbidden, map[string]string{
				"error": "missing or incorrect token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- Middleware Chain ----

type Middleware func(http.Handler) http.Handler

func MiddlewareChain(middleware ...Middleware) Middleware {
	return func(final http.Handler) http.Handler {
		for i := len(middleware) - 1; i >= 0; i-- {
			final = middleware[i](final)
		}
		return final
	}
}
