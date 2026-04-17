package receivemode

import (
	"errors"
	"io"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"

	"github.com/pgrwl/pgrwl/internal/opt/jobq"
)

type ReceiveController struct {
	Service Service
}

func NewReceiveController(s Service) *ReceiveController {
	return &ReceiveController{
		Service: s,
	}
}

func (c *ReceiveController) StatusHandler(w http.ResponseWriter, _ *http.Request) {
	status := c.Service.Status()
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (c *ReceiveController) BriefConfig(w http.ResponseWriter, r *http.Request) {
	briefConfig := c.Service.BriefConfig(r.Context())
	httpx.WriteJSON(w, http.StatusOK, briefConfig)
}

func (c *ReceiveController) DeleteWALsBeforeHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := httpx.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	if err := c.Service.DeleteWALsBefore(r.Context(), filename); err != nil {
		if errors.Is(err, jobq.ErrJobQueueFull) {
			httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "scheduled"})
}

// GetReceiverHandler handles GET /receiver.
// Returns the current receiver state and stream status.
func (c *ReceiveController) GetReceiverHandler(w http.ResponseWriter, _ *http.Request) {
	resp := ReceiverStateResponse{
		State:        c.Service.ReceiverState(),
		StreamStatus: c.Service.ReceiverStatus(),
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// StartReceiverHandler handles POST /receiver/states/running.
// Starts WAL streaming. Returns 409 if the receiver is already running.
func (c *ReceiveController) StartReceiverHandler(w http.ResponseWriter, _ *http.Request) {
	if err := c.Service.ReceiverStart(); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	resp := ReceiverStateResponse{
		State:        c.Service.ReceiverState(),
		StreamStatus: c.Service.ReceiverStatus(),
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// StopReceiverHandler handles POST /receiver/states/stopped.
// Stops WAL streaming and waits for the streaming goroutine to exit.
// Idempotent: returns 200 even if the receiver is already stopped.
func (c *ReceiveController) StopReceiverHandler(w http.ResponseWriter, _ *http.Request) {
	c.Service.ReceiverStop()
	resp := ReceiverStateResponse{
		State:        c.Service.ReceiverState(),
		StreamStatus: c.Service.ReceiverStatus(),
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// WalFileDownloadHandler handles GET /wal/{filename}.
// Used by restore_command on the standby.
func (c *ReceiveController) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := httpx.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	file, err := c.Service.GetWalFile(r.Context(), filename)
	if err != nil {
		http.Error(w, "file not found locally", http.StatusNotFound)
		return
	}
	defer file.Close()

	// TODO: send checksum in headers

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusInternalServerError)
		return
	}
}
