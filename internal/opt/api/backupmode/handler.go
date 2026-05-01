package backupmode

import (
	"net/http"

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

func (c *Handler) Start(w http.ResponseWriter, r *http.Request) {
	status, err := c.Service.Start()
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, err)
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (c *Handler) Status(w http.ResponseWriter, r *http.Request) {
	status := c.Service.Status()
	httpx.WriteJSON(w, http.StatusOK, status)
}
