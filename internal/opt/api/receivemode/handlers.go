package receivemode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	middleware2 "github.com/pgrwl/pgrwl/internal/opt/api/middleware"
	"golang.org/x/time/rate"
)

func initHandlers(cfg *config.Config, controller *ReceiveController) http.Handler {
	l := slog.With("component", "receive-api")

	// init middlewares
	loggingMiddleware := middleware2.LoggingMiddleware{
		Logger: l,
	}
	rateLimitMiddleware := middleware2.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware2.Chain(
		middleware2.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Streaming mode (requires that wal-streaming process is running)
	mux.Handle("/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("/config", secureChain(http.HandlerFunc(controller.BriefConfig)))
	mux.Handle("DELETE /wal-before/{filename}", secureChain(http.HandlerFunc(controller.DeleteWALsBeforeHandler)))

	api.InitOptionalHandlers(cfg, mux, l)
	return mux
}
