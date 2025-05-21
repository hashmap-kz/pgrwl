package httpsrv

import (
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	controlCrt "github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/controller"
	controlSvc "github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/service"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/middleware"

	"golang.org/x/time/rate"
)

type HTTPHandlersOpts struct {
	PGRW        xlog.PgReceiveWal
	BaseDir     string
	Verbose     bool
	RunningMode string
	Storage     *storage.TransformingStorage
}

func InitHTTPHandlers(opts *HTTPHandlersOpts) http.Handler {
	service := controlSvc.NewControlService(&controlSvc.ControlServiceOpts{
		PGRW:        opts.PGRW,
		BaseDir:     opts.BaseDir,
		RunningMode: opts.RunningMode,
		Storage:     opts.Storage,
	})
	controller := controlCrt.NewController(service)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  slog.With("component", "rest-api"),
		Verbose: opts.Verbose,
	}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.MiddlewareChain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)
	plainChain := middleware.MiddlewareChain(
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
	mux.Handle("POST /retention", secureChain(http.HandlerFunc(controller.RetentionHandler)))

	// Standalone mode (i.e. just serving wal-archive during restore)
	mux.Handle("/archive/size", secureChain(http.HandlerFunc(controller.ArchiveSizeHandler)))
	mux.Handle("/wal/{filename}", plainChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	return mux
}
