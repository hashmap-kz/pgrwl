package receivemode

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/middleware"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"golang.org/x/time/rate"
)

type Opts struct {
	PGRW     xlog.PgReceiveWal
	BaseDir  string
	Verbose  bool
	Storage  *storage.TransformingStorage
	JobQueue *jobq.JobQueue     // optional, nil in 'serve' mode
	StopFn   context.CancelFunc // stop main-receive loop outside on purpose
}

func Init(opts *Opts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "receive-api")

	service := NewReceiveModeService(&ReceiveServiceOpts{
		PGRW:     opts.PGRW,
		BaseDir:  opts.BaseDir,
		Storage:  opts.Storage,
		JobQueue: opts.JobQueue,
		Verbose:  opts.Verbose,
	})
	controller := NewReceiveController(service, opts.StopFn)

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

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Streaming mode (requires that wal-streaming process is running)
	mux.Handle("/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("/config", secureChain(http.HandlerFunc(controller.BriefConfig)))
	mux.Handle("DELETE /receiver", secureChain(http.HandlerFunc(controller.StopReceiver)))
	mux.Handle("DELETE /wal-before/{filename}", secureChain(http.HandlerFunc(controller.DeleteWALsBeforeHandler)))

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
