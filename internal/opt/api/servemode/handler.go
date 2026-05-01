package servemode

import (
	"io"
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/shared/x/httpx"
)

type Handler struct {
	Service Service
}

func NewHandler(s Service) *Handler {
	return &Handler{
		Service: s,
	}
}

func (c *Handler) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
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
