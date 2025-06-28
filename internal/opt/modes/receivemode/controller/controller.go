package controller

import (
	"errors"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/httpx"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/receivemode/service"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
)

type ReceiveController struct {
	Service service.ReceiveModeService
}

func NewReceiveController(s service.ReceiveModeService) *ReceiveController {
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
