package combinedmode

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
)

// ReceiverStateRequest is the JSON body for POST /receiver.
type ReceiverStateRequest struct {
	// State must be "running" or "stopped".
	State xlog.ReceiverState `json:"state"`
}

// ReceiverStateResponse is returned by GET /receiver and POST /receiver.
type ReceiverStateResponse struct {
	State        xlog.ReceiverState `json:"state"`
	StreamStatus *xlog.StreamStatus `json:"stream_status,omitempty"`
}

type Controller struct {
	Service Service
}

func NewController(s Service) *Controller {
	return &Controller{Service: s}
}

// GetReceiverHandler handles GET /receiver.
// Returns the current receiver state and streaming status.
func (c *Controller) GetReceiverHandler(w http.ResponseWriter, _ *http.Request) {
	resp := ReceiverStateResponse{
		State:        c.Service.ReceiverState(),
		StreamStatus: c.Service.ReceiverStatus(),
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// SetReceiverHandler handles POST /receiver.
//
// Request body: {"state": "running"} or {"state": "stopped"}
//
// Transitions:
//
//	stopped  -> running : starts WAL streaming
//	running  -> stopped : stops WAL streaming (waits for goroutine)
//	same -> same        : returns 200 with current status (idempotent)
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
		c.Service.ReceiverStop() // idempotent, no error

	default:
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": `state must be "running" or "stopped"`,
		})
		return
	}

	resp := ReceiverStateResponse{
		State:        c.Service.ReceiverState(),
		StreamStatus: c.Service.ReceiverStatus(),
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// WalFileDownloadHandler handles GET /wal/{filename}.
// Used by restore_command on the standby.
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
		http.Error(w, "cannot read file", http.StatusInternalServerError)
	}
}
