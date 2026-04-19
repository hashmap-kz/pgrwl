package servemode

import (
	"io"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
)

type ServeController struct {
	Service Service
}

func NewServeController(s Service) *ServeController {
	return &ServeController{
		Service: s,
	}
}

func (c *ServeController) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	filename, err := httpx.PathValueString(r, "filename")
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
