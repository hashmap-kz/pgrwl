package receivemode

import (
	"errors"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/httpx"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
)

type ReceiveController struct {
	Service  Service
	Pipeline *ReceivePipelineService
}

func NewReceiveController(s Service, p *ReceivePipelineService) *ReceiveController {
	return &ReceiveController{
		Service:  s,
		Pipeline: p,
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

func (c *ReceiveController) PauseStreaming(w http.ResponseWriter, _ *http.Request) {
	c.Pipeline.Pause()
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (c *ReceiveController) ResumeStreaming(w http.ResponseWriter, _ *http.Request) {
	c.Pipeline.Resume()
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}
