package controller

import (
	"io"
	"net/http"

	serveModeSvc "github.com/hashmap-kz/pgrwl/internal/opt/modes/serve/service"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ServeModeController struct {
	Service serveModeSvc.ServeModeService
}

func NewServeModeController(s serveModeSvc.ServeModeService) *ServeModeController {
	return &ServeModeController{
		Service: s,
	}
}

func (c *ServeModeController) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := optutils.PathValueString(r, "filename")
	if err != nil {
		http.Error(w, "expect filename path-param", http.StatusBadRequest)
		return
	}

	file, err := c.Service.GetWalFile(r.Context(), filename)
	if err != nil {
		http.Error(w, "file not found locally", http.StatusNotFound)
		return
	}
	defer file.Close()

	// TODO: send checksum in headers

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusInternalServerError)
		return
	}
}
