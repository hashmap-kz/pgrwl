package receivemode

import (
	"errors"
	"io"
	"net/http"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/httpx"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
)

type ReceiveController struct {
	Service Service
	opts    *ReceiveDaemonRunOpts
}

func NewReceiveController(s Service, opts *ReceiveDaemonRunOpts) *ReceiveController {
	return &ReceiveController{
		Service: s,
		opts:    opts,
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

func (c *ReceiveController) WalFileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if c.opts.ReceiverController.IsRunning() {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"status": "error",
			"reason": "cannon safely fetch partial files, receiver in running, stop receiver and try again",
		})
		return
	}

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

// daemons

func (c *ReceiveController) receiverStart(w http.ResponseWriter, _ *http.Request) {
	c.opts.ReceiverController.Start()
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) receiverStop(w http.ResponseWriter, _ *http.Request) {
	c.opts.ReceiverController.Stop()
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) archiverStart(w http.ResponseWriter, _ *http.Request) {
	if c.opts.ArchiveController != nil {
		c.opts.ArchiveController.Start()
	}
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) archiverStop(w http.ResponseWriter, _ *http.Request) {
	if c.opts.ArchiveController != nil {
		c.opts.ArchiveController.Stop()
	}
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) daemonsStart(w http.ResponseWriter, _ *http.Request) {
	c.opts.ReceiverController.Start()
	if c.opts.ArchiveController != nil {
		c.opts.ArchiveController.Start()
	}
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) daemonsStop(w http.ResponseWriter, _ *http.Request) {
	c.opts.ReceiverController.Stop()
	if c.opts.ArchiveController != nil {
		c.opts.ArchiveController.Stop()
	}
	httpx.WriteJSON(w, http.StatusOK, statusOk)
}

func (c *ReceiveController) daemonsStatus(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]string{
		"receiver": c.opts.ReceiverController.Status(),
	}
	if c.opts.ArchiveController != nil {
		resp["archiver"] = c.opts.ArchiveController.Status()
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
