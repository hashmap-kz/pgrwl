package combinedmode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	"github.com/pgrwl/pgrwl/internal/opt/shared/middleware"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"golang.org/x/time/rate"
)

// HandlerOpts groups everything Init() needs.
type HandlerOpts struct {
	Receiver *xlog.RestartablePgReceiver
	BaseDir  string
	Verbose  bool
	Storage  *st.VariadicStorage
}

// Init wires up the combined-mode HTTP mux.
//
// Routes registered:
//
//	GET  /healthz                    - liveness probe
//	GET  /receiver                   - current receiver state + stream status
//	POST /receiver                   - switch receiver state (running / stopped)
//	GET  /wal/{filename}             - WAL file download (restore_command)
//	GET  /status                     - stream status (legacy receive-mode compat)
//	DELETE /wal-before/{filename}    - schedule WAL deletion (requires jobqueue; omitted here)
//	GET  /metrics                    - Prometheus (if enabled)
//	GET  /debug/pprof/               - pprof (if enabled)
func Init(opts *HandlerOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "combined-api")

	svc := NewService(&ServiceOpts{
		Receiver: opts.Receiver,
		BaseDir:  opts.BaseDir,
		Storage:  opts.Storage,
	})
	ctrl := NewController(svc)

	loggingMW := middleware.LoggingMiddleware{Logger: l, Verbose: opts.Verbose}
	rateLimitMW := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	secureChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMW.Middleware,
		rateLimitMW.Middleware,
	)
	plainChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMW.Middleware,
	)

	mux := http.NewServeMux()

	// Liveness
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Receiver control
	mux.Handle("GET /receiver", secureChain(http.HandlerFunc(ctrl.GetReceiverHandler)))
	mux.Handle("POST /receiver", secureChain(http.HandlerFunc(ctrl.SetReceiverHandler)))

	// WAL serving (for restore_command / standby)
	mux.Handle("GET /wal/{filename}", plainChain(http.HandlerFunc(ctrl.WalFileDownloadHandler)))

	// Legacy receive-mode status endpoint (backwards compat)
	mux.Handle("GET /status", secureChain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctrl.GetReceiverHandler(w, r)
	})))

	// Prometheus + pprof (conditional)
	shared.InitOptionalHandlers(cfg, mux, l)

	return mux
}
