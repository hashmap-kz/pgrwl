package receivemode

import (
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
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
