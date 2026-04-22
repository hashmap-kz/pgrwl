package backupmode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
)

func initHandlers(cfg *config.Config) http.Handler {
	l := slog.With("component", "basebackup-api")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	api.InitOptionalHandlers(cfg, mux, l)
	return mux
}
