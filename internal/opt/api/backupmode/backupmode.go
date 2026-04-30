package backupmode

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"
)

type BackupController interface {
	Start(ctx context.Context) (*backupsv.BackupRunState, error)
	Status() backupsv.BackupRunState
}

type Opts struct {
	Cfg        *config.Config
	Controller BackupController
}

func Init(opts *Opts) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/basebackup", func(w http.ResponseWriter, r *http.Request) {
		if opts == nil || opts.Controller == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_configured",
				"error":  "backup controller is not configured",
			})
			return
		}

		switch r.Method {
		case http.MethodPost:
			state, err := opts.Controller.Start(r.Context())
			if err != nil {
				writeStartBackupError(w, err)
				return
			}

			writeJSON(w, http.StatusAccepted, state)

		case http.MethodGet:
			writeJSON(w, http.StatusOK, opts.Controller.Status())

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"status": "method_not_allowed",
				"error":  "method not allowed",
			})
		}
	})

	mux.HandleFunc("/api/v1/basebackup/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"status": "method_not_allowed",
				"error":  "method not allowed",
			})
			return
		}

		if opts == nil || opts.Controller == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_configured",
				"error":  "backup controller is not configured",
			})
			return
		}

		writeJSON(w, http.StatusOK, opts.Controller.Status())
	})

	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"status": "method_not_allowed",
				"error":  "method not allowed",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
		})
	})

	return mux
}

func writeStartBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backupsv.ErrBackupAlreadyRunning):
		writeJSON(w, http.StatusConflict, map[string]any{
			"status": "already_running",
			"error":  err.Error(),
		})
		return

	case errors.Is(err, context.Canceled):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "stopping",
			"error":  err.Error(),
		})
		return

	default:
		slog.Error("manual basebackup trigger failed", slog.Any("err", err))

		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json response failed", slog.Any("err", err))
	}
}
