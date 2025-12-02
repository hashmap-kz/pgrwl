package receivemode

import (
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/httpx"
	"github.com/hashmap-kz/pgrwl/internal/opt/wrk"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/middleware"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"golang.org/x/time/rate"
)

type ReceiveHandlerOpts struct {
	PGRW     xlog.PgReceiveWal
	BaseDir  string
	Verbose  bool
	Storage  *storage.VariadicStorage
	JobQueue *jobq.JobQueue // optional, nil in 'serve' mode

	ReceiverController *wrk.WorkerController
	ArchiveController  *wrk.WorkerController
}

var statusOk = map[string]string{
	"status": "ok",
}

func Init(opts *ReceiveHandlerOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "receive-api")

	service := NewReceiveModeService(&ReceiveServiceOpts{
		PGRW:     opts.PGRW,
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

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Streaming mode (requires that wal-streaming process is running)
	mux.Handle("/status", secureChain(http.HandlerFunc(controller.StatusHandler)))
	mux.Handle("/config", secureChain(http.HandlerFunc(controller.BriefConfig)))
	mux.Handle("DELETE /wal-before/{filename}", secureChain(http.HandlerFunc(controller.DeleteWALsBeforeHandler)))

	// control endpoints

	mux.HandleFunc("POST /api/v1/daemons/receiver/start", func(w http.ResponseWriter, _ *http.Request) {
		opts.ReceiverController.Start()
		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"status": opts.ReceiverController.Status(),
		})
	})

	mux.HandleFunc("POST /api/v1/daemons/receiver/stop", func(w http.ResponseWriter, _ *http.Request) {
		opts.ReceiverController.Stop()
		httpx.WriteJSON(w, http.StatusOK, statusOk)
	})

	if opts.ArchiveController != nil {
		mux.HandleFunc("POST /api/v1/daemons/archiver/start", func(w http.ResponseWriter, _ *http.Request) {
			opts.ArchiveController.Start()
			httpx.WriteJSON(w, http.StatusOK, statusOk)
		})

		mux.HandleFunc("POST /api/v1/daemons/archiver/stop", func(w http.ResponseWriter, _ *http.Request) {
			opts.ArchiveController.Stop()
			httpx.WriteJSON(w, http.StatusOK, statusOk)
		})
	}

	mux.HandleFunc("POST /api/v1/daemons/stop", func(w http.ResponseWriter, _ *http.Request) {
		opts.ReceiverController.Stop()
		if opts.ArchiveController != nil {
			opts.ArchiveController.Stop()
		}
		httpx.WriteJSON(w, http.StatusOK, statusOk)
	})

	mux.HandleFunc("POST /api/v1/daemons/start", func(w http.ResponseWriter, _ *http.Request) {
		opts.ReceiverController.Start()
		if opts.ArchiveController != nil {
			opts.ArchiveController.Start()
		}
		httpx.WriteJSON(w, http.StatusOK, statusOk)
	})

	mux.HandleFunc("GET /api/v1/daemons/status", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]string{
			"receiver": opts.ReceiverController.Status(),
		}
		if opts.ArchiveController != nil {
			resp["archiver"] = opts.ArchiveController.Status()
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	})

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
