package backupmode

import (
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
)

func Init() http.Handler {
	cfg := config.Cfg()
	l := slog.With("component", "basebackup")

	// Init handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	shared.InitOptionalHandlers(cfg, mux, l)
	return mux
}
