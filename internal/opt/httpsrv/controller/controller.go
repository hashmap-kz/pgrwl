package controller

import (
	"archive/tar"
	"errors"
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

	// TODO: local storage

	file, err := c.Service.GetWalFile(r.Context(), filename+".tar")
	if err != nil {
		http.Error(w, "file not found locally", http.StatusNotFound)
		return
	}
	defer file.Close()

	// TODO: send checksum in headers
	readCloser, err := GetFileFromTar(file, filename)
	if err != nil {
		http.Error(w, "file not found in tar", http.StatusNotFound)
		return
	}
	defer readCloser.Close()

	_, err = io.Copy(w, readCloser)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusInternalServerError)
		return
	}
}

// GetFileFromTar returns a ReadCloser for a specific file inside a tar stream.
// The caller must close the returned ReadCloser.
func GetFileFromTar(r io.Reader, target string) (io.ReadCloser, error) {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("file not found in tar")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == target {
			// Wrap tar.Reader in ReadCloser so caller can close
			pr, pw := io.Pipe()

			go func() {
				defer pw.Close()
				_, err := io.Copy(pw, tr)
				if err != nil {
					_ = pw.CloseWithError(err)
				}
			}()

			return pr, nil
		}
	}
}
