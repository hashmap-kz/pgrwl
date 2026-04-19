package rest

import (
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"

	"github.com/pgrwl/pgrwl/config"
)

// MountModeRoutes registers /mode endpoints on mux.
// These are on the top-level mux (not ModeRouter) so they are always
// reachable regardless of which mode is active.
func MountModeRoutes(mux *http.ServeMux, mgr *Manager) {
	mux.HandleFunc("GET /mode", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"mode": mgr.CurrentMode()})
	})

	mux.HandleFunc("POST /mode/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name != config.ModeReceive && name != config.ModeServe {
			http.Error(w, "valid modes: receive, serve", http.StatusBadRequest)
			return
		}
		if err := mgr.Switch(name); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"mode": name})
	})
}
