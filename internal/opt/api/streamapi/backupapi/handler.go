package backupapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
)

type Handler struct {
	Service Service
}

func NewBackupHandler(s Service) *Handler {
	return &Handler{
		Service: s,
	}
}

func (c *Handler) Start(w http.ResponseWriter, _ *http.Request) {
	status, err := c.Service.Start()
	if err != nil {
		writeStartBackupError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (c *Handler) Status(w http.ResponseWriter, _ *http.Request) {
	status := c.Service.Status()
	httpx.WriteJSON(w, http.StatusOK, status)
}

func writeStartBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backupsv.ErrBackupAlreadyRunning):
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"status": "already_running",
			"error":  err.Error(),
		})
		return

	case errors.Is(err, context.Canceled):
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "stopping",
			"error":  err.Error(),
		})
		return

	default:
		slog.Error("manual basebackup trigger failed", slog.Any("err", err))

		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
}
