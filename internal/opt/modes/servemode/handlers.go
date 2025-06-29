package servemode

import (
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/middleware"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

type HandlerOpts struct {
	BaseDir string
	Verbose bool
	Storage *storage.TransformingStorage
}

func Init(opts *HandlerOpts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "serve-api")

	service := NewServeModeService(&SvcOpts{
		BaseDir: opts.BaseDir,
		Storage: opts.Storage,
		Verbose: opts.Verbose,
	})
	controller := NewServeModeController(service)

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger:  l,
		Verbose: opts.Verbose,
	}

	plainChain := middleware.Chain(
		middleware.SafeHandlerMiddleware,
		loggingMiddleware.Middleware,
	)

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Standalone mode (i.e. just serving wal-archive during restore)
	mux.Handle("/wal/{filename}", plainChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
