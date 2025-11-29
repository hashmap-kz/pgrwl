package servemode

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/httpx"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backupmode"
)

type BackupRetriever struct {
	cfg *config.Config
}

type restoreRequest struct {
	// Backup ID; if empty, RestoreBaseBackup will pick the latest
	ID string `json:"id"`

	// Restore destination
	Dest string `json:"dest"`
}

func (s *BackupRetriever) handleRestore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req restoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}
	if req.ID == "" || req.Dest == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "id and dest cannot be empty",
		})
		return
	}

	slog.Info("restore requested",
		slog.String("id", req.ID),
		slog.String("dest", req.Dest),
	)

	// Do the restore synchronously for now
	if err := backupmode.RestoreBaseBackup(ctx, s.cfg, req.ID, req.Dest); err != nil {
		slog.Error("restore failed",
			slog.String("id", req.ID),
			slog.String("dest", req.Dest),
			slog.Any("err", err),
		)
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"status": "failed",
			"reason": err.Error(),
		})
		return
	}

	slog.Info("restore completed",
		slog.String("id", req.ID),
		slog.String("dest", req.Dest),
	)

	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"id":     req.ID,
		"dest":   req.Dest,
	})
}
