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
	ID string `json:"id,omitempty"`

	// Restore destination
	Dest string `json:"dest,omitempty"`
}

type restoreResponse struct {
	ID   string `json:"id"`
	Dest string `json:"dest"`
	Msg  string `json:"msg"`
}

func (s *BackupRetriever) handleRestore(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req restoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// TODO: fix (should be an empty dir)
	// Decide destination (I’d strongly recommend not letting client pick arbitrary path)
	dest := req.Dest

	slog.Info("restore requested",
		slog.String("id", req.ID),
		slog.String("dest", dest),
	)

	// Do the restore synchronously for now
	if err := backupmode.RestoreBaseBackup(ctx, s.cfg, req.ID, dest); err != nil {
		slog.Error("restore failed",
			slog.String("id", req.ID),
			slog.String("dest", dest),
			slog.Any("err", err),
		)
		http.Error(w, "restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("restore completed",
		slog.String("id", req.ID),
		slog.String("dest", dest),
	)

	httpx.WriteJSON(w, http.StatusOK, restoreResponse{
		ID:   req.ID,
		Dest: dest,
		Msg:  "restore completed",
	})
}
