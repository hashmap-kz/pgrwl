package receivemode

import (
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/middleware"

	"github.com/hashmap-kz/pgrwl/config"

	"golang.org/x/time/rate"
)

var statusOk = map[string]string{
	"status": "ok",
}

func Init(opts *ReceiveDaemonRunOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "receive-api")

	service := NewReceiveModeService(opts)
	controller := NewReceiveController(service, opts)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  l,
		Verbose: opts.Verbose,
	}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)

	plainChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
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

	// Standalone mode (i.e. just serving wal-archive during restore)
	mux.Handle("/wal/{filename}", plainChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	// control endpoints

	mux.Handle("POST /api/v1/daemons/receiver/start", secureChain(http.HandlerFunc(controller.receiverStart)))
	mux.Handle("POST /api/v1/daemons/receiver/stop", secureChain(http.HandlerFunc(controller.receiverStop)))
	mux.Handle("POST /api/v1/daemons/archiver/start", secureChain(http.HandlerFunc(controller.archiverStart)))
	mux.Handle("POST /api/v1/daemons/archiver/stop", secureChain(http.HandlerFunc(controller.archiverStop)))
	mux.Handle("POST /api/v1/daemons/start", secureChain(http.HandlerFunc(controller.daemonsStart)))
	mux.Handle("POST /api/v1/daemons/stop", secureChain(http.HandlerFunc(controller.daemonsStop)))
	mux.Handle("POST /api/v1/switch-to-wal-receive", secureChain(http.HandlerFunc(controller.daemonsStart)))
	mux.Handle("POST /api/v1/switch-to-wal-serve", secureChain(http.HandlerFunc(controller.daemonsStop)))
	mux.Handle("GET /api/v1/daemons/status", secureChain(http.HandlerFunc(controller.daemonsStatus)))

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
