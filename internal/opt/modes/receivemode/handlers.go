package receivemode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	"github.com/pgrwl/pgrwl/internal/opt/shared/middleware"

	"github.com/pgrwl/pgrwl/config"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"golang.org/x/time/rate"
)

type ReceiveHandlerOpts struct {
	Receiver *xlog.RestartablePgReceiver
	BaseDir  string
	Verbose  bool
	Storage  *st.VariadicStorage
	JobQueue *jobq.JobQueue // optional, nil in 'serve' mode
}

func Init(opts *ReceiveHandlerOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "receive-api")

	service := NewReceiveModeService(&ReceiveServiceOpts{
		PGRW:     opts.Receiver,
		BaseDir:  opts.BaseDir,
		Storage:  opts.Storage,
		JobQueue: opts.JobQueue,
		Verbose:  opts.Verbose,
	})
	controller := NewReceiveController(service)

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

	// Receiver lifecycle control
	mux.Handle("GET /receiver", secureChain(http.HandlerFunc(controller.GetReceiverHandler)))
	mux.Handle("POST /receiver/states/running", secureChain(http.HandlerFunc(controller.StartReceiverHandler)))
	mux.Handle("POST /receiver/states/stopped", secureChain(http.HandlerFunc(controller.StopReceiverHandler)))

	// WAL file serving (for restore_command / standby)
	mux.Handle("GET /wal/{filename}", plainChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
