package backupmode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
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
