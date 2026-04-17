package receivemode

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
)

type Controller struct {
	Service Service
}

func NewController(s Service) *Controller {
	return &Controller{Service: s}
}

// GetReceiverHandler handles GET /receiver.
// Returns the current receiver state and streaming status.
func (c *Controller) GetReceiverHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, c.Service.ReceiverStatus())
}

// SetReceiverHandler handles POST /receiver.
//
//	{"state":"running"} - start WAL streaming
//	{"state":"stopped"} - stop WAL streaming
//
// Idempotent: sending the current state returns 200 without error.
func (c *Controller) SetReceiverHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil {
		http.Error(w, "cannot read request body", http.StatusBadRequest)
		return
	}

	var req ReceiverStateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	switch req.State {
	case xlog.ReceiverStateRunning:
		if err := c.Service.ReceiverStart(); err != nil {
			httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
	case xlog.ReceiverStateStopped:
		c.Service.ReceiverStop() // always idempotent
	default:
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": `state must be "running" or "stopped"`,
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, c.Service.ReceiverStatus())
}

// WalFileDownloadHandler handles GET /wal/{filename}.
// Called by `pgrwl restore-command` during PostgreSQL WAL replay.
func (c *Controller) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := httpx.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	file, err := c.Service.GetWalFile(r.Context(), filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	if _, err := io.Copy(w, file); err != nil {
		// Header already sent; nothing useful to do except log.
		http.Error(w, "stream error", http.StatusInternalServerError)
	}
}

// StatusHandler handles GET /status (kept for backwards compatibility).
func (c *Controller) StatusHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, c.Service.ReceiverStatus())
}

// BriefConfigHandler handles GET /config.
func (c *Controller) BriefConfigHandler(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, c.Service.BriefConfig(r.Context()))
}

// DeleteWALsBeforeHandler handles DELETE /wal-before/{filename}.
func (c *Controller) DeleteWALsBeforeHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := httpx.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	if err := c.Service.DeleteWALsBefore(r.Context(), filename); err != nil {
		if isJobQueueFull(err) {
			httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "scheduled"})
}

func isJobQueueFull(err error) bool {
	return errors.Is(err, jobq.ErrJobQueueFull)
}
