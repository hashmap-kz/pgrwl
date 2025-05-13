package controller

import (
	"io"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/service"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ControlController struct {
	Service service.ControlService
}

func NewController(s service.ControlService) *ControlController {
	return &ControlController{
		Service: s,
	}
}

func (c *ControlController) StatusHandler(w http.ResponseWriter, _ *http.Request) {
	status := c.Service.Status()
	optutils.WriteJSON(w, http.StatusOK, status)
}

func (c *ControlController) ArchiveSizeHandler(w http.ResponseWriter, _ *http.Request) {
	sizeInfo, err := c.Service.WALArchiveSize()
	if err != nil {
		optutils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	optutils.WriteJSON(w, http.StatusOK, sizeInfo)
}

func (c *ControlController) RetentionHandler(w http.ResponseWriter, _ *http.Request) {
	if err := c.Service.RetainWALs(); err != nil {
		optutils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	optutils.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleanup done"})
}

func (c *ControlController) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := optutils.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	f, err := c.Service.GetWalFile(filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	_, err = io.Copy(w, f)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
}
