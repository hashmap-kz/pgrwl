package app

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/api/backupmode"
	"github.com/pgrwl/pgrwl/internal/opt/api/middleware"
	"github.com/pgrwl/pgrwl/internal/opt/api/receivemode"
	"golang.org/x/time/rate"
)

type Opts struct {
	Receive *receivemode.Opts
	Backup  *backupmode.Opts
	Cfg     *config.Config
}

type HandlerV1 struct {
	Receive receivemode.Handler
	Backup  backupmode.Handler
}

type Service struct {
	Receive receivemode.Service
	Backup  backupmode.Service
}

func Init(o *Opts) http.Handler {
	// init services/handlers
	backupSvc := backupmode.NewBackupService(o.Backup)
	backupHdl := backupmode.NewBackupHandler(backupSvc)
	receiveSvc := receivemode.NewService(o.Receive)
	receiveHld := receivemode.NewHandler(receiveSvc)

	l := slog.With("component", "receive-api")

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

	api.InitOptionalHandlers(o.Cfg, mux, l)
	return mux
}
