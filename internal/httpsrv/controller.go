package httpsrv

import (
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/httpsrv/httputils"
)

type ControlController struct {
	Service *ControlService
	lock    chan struct{}
}

func NewController(s *ControlService) *ControlController {
	return &ControlController{
		Service: s,
		lock:    make(chan struct{}, 1),
	}
}

func (c *ControlController) StatusHandler(w http.ResponseWriter, _ *http.Request) {
	status := c.Service.Status()
	httputils.WriteJSON(w, http.StatusOK, status)
}

func (c *ControlController) ArchiveSizeHandler(w http.ResponseWriter, _ *http.Request) {
	sizeInfo, err := c.Service.WALArchiveSize()
	if err != nil {
		httputils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httputils.WriteJSON(w, http.StatusOK, sizeInfo)
}

func (c *ControlController) RetentionHandler(w http.ResponseWriter, _ *http.Request) {
	select {
	case c.lock <- struct{}{}:
		defer func() { <-c.lock }()
	default:
		httputils.WriteJSON(w, http.StatusConflict, map[string]string{
			"error": "retention already in progress",
		})
		return
	}

	if err := c.Service.RetainWALs(); err != nil {
		httputils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httputils.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleanup done"})
}
