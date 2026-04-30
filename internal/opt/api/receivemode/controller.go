package receivemode

import (
	"errors"
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
	briefConfig, err := c.Service.BriefConfig(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, err)
	}
	httpx.WriteJSON(w, http.StatusOK, briefConfig)
}

func (c *ReceiveController) FullRedactedConfig(w http.ResponseWriter, r *http.Request) {
	briefConfig := c.Service.FullRedactedConfig(r.Context())
	httpx.WriteJSON(w, http.StatusOK, briefConfig)
}

func (c *ReceiveController) SnapshotHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := c.Service.Snapshot(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, err)
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

func (c *ReceiveController) WalsHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := c.Service.ListWALFiles(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"err": err.Error(),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
}

func (c *ReceiveController) BackupsHandler(w http.ResponseWriter, r *http.Request) {
	snap, err := c.Service.ListBackups(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"err": err.Error(),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, snap)
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
