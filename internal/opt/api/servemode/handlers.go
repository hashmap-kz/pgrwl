package servemode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/api/middleware"
)

func initHandlers(controller *ServeController) http.Handler {
	l := slog.With("component", "serve-api")

	// init middlewares
	loggingMiddleware := middleware.LoggingMiddleware{
		Logger: l,
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
	mux.Handle("/api/v1/wal/{filename}", plainChain(http.HandlerFunc(controller.WalFileDownloadHandler)))

	return mux
}
