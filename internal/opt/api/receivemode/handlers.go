package receivemode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/api/middleware"
	"golang.org/x/time/rate"
)

func initHandlers(cfg *config.Config, controller *ReceiveController) http.Handler {
	l := slog.With("component", "receive-api")

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger: l,
	}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Streaming mode (requires that wal-streaming process is running)
	mux.Handle("GET /api/v1/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("GET /api/v1/brief-config", secureChain(http.HandlerFunc(controller.BriefConfig)))
	mux.Handle("GET /api/v1/redacted-config", secureChain(http.HandlerFunc(controller.FullRedactedConfig)))
	mux.Handle("DELETE /api/v1/wal-before/{filename}", secureChain(http.HandlerFunc(controller.DeleteWALsBeforeHandler)))

	api.InitOptionalHandlers(cfg, mux, l)
	return mux
}
