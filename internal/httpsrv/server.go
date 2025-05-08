package httpsrv

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/httpsrv/middleware"

	"github.com/hashmap-kz/pgrwl/internal/xlog"

	"golang.org/x/time/rate"
)

// ---- Server ----

type HTTPServer struct {
	srv     *http.Server
	logger  *slog.Logger
	pgrw    *xlog.PgReceiveWal
	verbose bool
}

// ---- Constructor ----

func NewHTTPServer(_ context.Context, addr string, pgrw *xlog.PgReceiveWal) *HTTPServer {
	h := &HTTPServer{
		logger:  slog.With("component", "http-server"),
		pgrw:    pgrw,
		verbose: pgrw.Verbose,
	}

	service := &ControlService{PGRW: pgrw}
	controller := NewController(service)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  h.logger,
		Verbose: pgrw.Verbose,
	}
	tokenAuthMiddleware := middleware.AuthMiddleware{Token: os.Getenv("PGRWL_AUTH_TOKEN")}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.MiddlewareChain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
		tokenAuthMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("POST /retention", secureChain(http.HandlerFunc(controller.RetentionHandler)))
	mux.Handle("/archive/size", secureChain(http.HandlerFunc(controller.ArchiveSizeHandler)))

	h.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	return h
}

// Start starts HTTP-server if it's not nil
func Start(_ context.Context, h *HTTPServer) {
	if h == nil {
		return
	}
	go func() {
		h.logger.Info("HTTP server listening", slog.String("addr", h.srv.Addr))
		if err := h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("HTTP server error", slog.Any("err", err))
		}
	}()
}

// Shutdown teardown HTTP-server if it's not nil
func Shutdown(ctx context.Context, h *HTTPServer) {
	if h == nil {
		return
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	h.logger.Info("shutting down HTTP server")
	if err := h.srv.Shutdown(timeoutCtx); err != nil {
		h.logger.Error("error during HTTP server shutdown", slog.Any("err", err))
	} else {
		h.logger.Info("HTTP server shut down cleanly")
	}
}
