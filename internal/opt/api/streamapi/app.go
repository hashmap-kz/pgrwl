package streamapi

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/pgrwl/pgrwl/internal/opt/api/streamapi/backupapi"
	"github.com/pgrwl/pgrwl/internal/opt/api/streamapi/receiveapi"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api/middleware"
	"golang.org/x/time/rate"
)

type Opts struct {
	Receive *receiveapi.Opts
	Backup  *backupapi.Opts
	Cfg     *config.Config
}

func Init(o *Opts) http.Handler {
	l := slog.With("component", "stream-api")

	// init services/handlers
	backupSvc := backupapi.NewBackupService(o.Backup)
	backupHdl := backupapi.NewBackupHandler(backupSvc)
	receiveSvc := receiveapi.NewService(o.Receive)
	receiveHld := receiveapi.NewHandler(receiveSvc)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger: l,
	}
	rateLimitMiddleware := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	// Build middleware chain
	secureChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		middleware.Cors,
		loggingMiddleware.Middleware,
		rateLimitMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// mount routes
	mux.Handle("POST /api/v1/basebackup", secureChain(http.HandlerFunc(backupHdl.Start)))
	mux.Handle("GET /api/v1/basebackup/status", secureChain(http.HandlerFunc(backupHdl.Status)))
	mux.Handle("GET /api/v1/status", secureChain(http.HandlerFunc(receiveHld.StatusHandler)))
	mux.Handle("GET /api/v1/brief-config", secureChain(http.HandlerFunc(receiveHld.BriefConfig)))
	mux.Handle("GET /api/v1/redacted-config", secureChain(http.HandlerFunc(receiveHld.FullRedactedConfig)))
	mux.Handle("GET /api/v1/snapshot", secureChain(http.HandlerFunc(receiveHld.SnapshotHandler)))
	mux.Handle("GET /api/v1/wals", secureChain(http.HandlerFunc(receiveHld.WalsHandler)))
	mux.Handle("GET /api/v1/backups", secureChain(http.HandlerFunc(receiveHld.BackupsHandler)))

	initOptionalHandlers(o.Cfg, mux, l)
	return mux
}

func initOptionalHandlers(cfg *config.Config, mux *http.ServeMux, l *slog.Logger) {
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
}
