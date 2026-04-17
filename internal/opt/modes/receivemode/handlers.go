package receivemode

import (
	"log/slog"
	"net/http"

	"golang.org/x/time/rate"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	"github.com/pgrwl/pgrwl/internal/opt/shared/middleware"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

// ReceiveHandlerOpts groups everything the HTTP layer needs at startup.
type ReceiveHandlerOpts struct {
	Receiver *xlog.RestartablePgReceiver
	BaseDir  string
	Verbose  bool
	Storage  *st.VariadicStorage
	JobQueue *jobq.JobQueue
}

// Init wires up the receive-mode HTTP mux.
//
// Routes:
//
//	GET  /healthz                  - liveness probe
//	GET  /receiver                 - current state + stream status
//	POST /receiver                 - start or stop WAL streaming
//	GET  /wal/{filename}           - WAL file download (restore_command)
//	GET  /status                   - alias for GET /receiver (backwards compat)
//	GET  /config                   - brief config (used by backup supervisor)
//	DELETE /wal-before/{filename}  - schedule WAL deletion
//	GET  /metrics                  - Prometheus (if enabled)
//	GET  /debug/pprof/             - pprof (if enabled)
func Init(opts *ReceiveHandlerOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "receive-api")

	svc := NewService(&ServiceOpts{
		Receiver: opts.Receiver,
		BaseDir:  opts.BaseDir,
		Storage:  opts.Storage,
		JobQueue: opts.JobQueue,
		Verbose:  opts.Verbose,
	})
	ctrl := NewController(svc)

	loggingMW := middleware.LoggingMiddleware{Logger: l, Verbose: opts.Verbose}
	rateLimitMW := middleware.RateLimiterMiddleware{Limiter: rate.NewLimiter(5, 10)}

	secureChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMW.Middleware,
		rateLimitMW.Middleware,
	)
	// WAL downloads are high-frequency and large; skip the rate limiter.
	plainChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMW.Middleware,
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Receiver lifecycle control
	mux.Handle("GET /receiver", secureChain(http.HandlerFunc(ctrl.GetReceiverHandler)))
	mux.Handle("POST /receiver", secureChain(http.HandlerFunc(ctrl.SetReceiverHandler)))

	// WAL file serving for restore_command
	mux.Handle("GET /wal/{filename}", plainChain(http.HandlerFunc(ctrl.WalFileDownloadHandler)))

	// Backwards-compatible aliases
	mux.Handle("GET /status", secureChain(http.HandlerFunc(ctrl.StatusHandler)))
	mux.Handle("GET /config", secureChain(http.HandlerFunc(ctrl.BriefConfigHandler)))

	// WAL archive management (called by backup supervisor)
	mux.Handle("DELETE /wal-before/{filename}", secureChain(http.HandlerFunc(ctrl.DeleteWALsBeforeHandler)))

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
