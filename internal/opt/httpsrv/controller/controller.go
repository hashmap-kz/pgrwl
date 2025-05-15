package controller

import (
	"net/http"
	"strconv"

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

	file, err := c.Service.GetWalFile(filename)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Get file info
	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.Error(w, "file stat error", http.StatusBadRequest)
		return
	}

	// Set headers
	w.Header().Set("Content-Disposition", "attachment; filename=\""+stat.Name()+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	// Serve content (efficient zero-copy if possible)
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
}
