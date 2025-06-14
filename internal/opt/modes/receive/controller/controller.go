package controller

import (
	"errors"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/receive/service"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
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
	optutils.WriteJSON(w, http.StatusOK, status)
}

func (c *ReceiveController) DeleteWALsBeforeHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := optutils.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	if err := c.Service.DeleteWALsBefore(r.Context(), filename); err != nil {
		if errors.Is(err, jobq.ErrJobQueueFull) {
			optutils.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		optutils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	optutils.WriteJSON(w, http.StatusOK, map[string]string{"status": "scheduled"})
}
