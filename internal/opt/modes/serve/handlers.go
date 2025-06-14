package receive

import (
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/compn"
	"github.com/hashmap-kz/pgrwl/internal/opt/compn/middleware"
	serveCtr "github.com/hashmap-kz/pgrwl/internal/opt/modes/serve/controller"
	serveSvc "github.com/hashmap-kz/pgrwl/internal/opt/modes/serve/service"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

type Opts struct {
	BaseDir string
	Verbose bool
	Storage *storage.TransformingStorage
}

func Init(opts *Opts) http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "serve-api")

	service := serveSvc.NewServeModeService(&serveSvc.Opts{
		BaseDir: opts.BaseDir,
		Storage: opts.Storage,
		Verbose: opts.Verbose,
	})
	controller := serveCtr.NewServeModeController(service)

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

	compn.InitOptionalHandlers(cfg, mux, l)
	return mux
}
