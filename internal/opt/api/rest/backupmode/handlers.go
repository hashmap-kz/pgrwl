package backupmode

import (
	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	"log/slog"
	"net/http"
)

func initHandlers(cfg *config.Config) http.Handler {
	loggr := slog.With("component", "basebackup-api")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	shared.InitOptionalHandlers(cfg, mux, loggr)
	return mux
}
