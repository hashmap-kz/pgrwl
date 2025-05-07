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

// TODO: NewHTTPServer, struct, logger, etc...

func StartHTTPServer(_ context.Context) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	secureChain := MiddlewareChain(
		safeHandlerMiddleware,
		loggingMiddleware,
		rateLimitMiddleware,
		tokenAuthMiddleware,
	)
	mux.Handle("/status", secureChain(http.HandlerFunc(statusHandler)))

	srv := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		slog.Info("HTTP server listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", slog.Any("err", err))
		}
	}()

	return srv
}

func ShutdownHTTPServer(srv *http.Server) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	slog.Info("shutting down HTTP server")
	if err := srv.Shutdown(ctxTimeout); err != nil {
		slog.Error("error during HTTP server shutdown", slog.Any("err", err))
	} else {
		slog.Info("HTTP server shut down cleanly")
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
