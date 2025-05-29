package httpsrv

import (
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	cfg := config.Cfg()
	l := slog.With("component", "rest-api")

	service := controlSvc.NewControlService(&controlSvc.ControlServiceOpts{
		PGRW:        opts.PGRW,
		BaseDir:     opts.BaseDir,
		RunningMode: opts.RunningMode,
		Storage:     opts.Storage,
	})
	controller := controlCrt.NewController(service)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  l,
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

	if cfg.Metrics.Enable {
		l.Debug("enable metric endpoints")

		mux.Handle("/metrics", promhttp.Handler())
	}

	if cfg.DevConfig.Pprof.Enable {
		l.Debug("enable pprof endpoints")

		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	return mux
}
