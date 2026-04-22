package servemode

import (
	"net/http"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type Opts struct {
	BaseDir string
	Storage *st.VariadicStorage
}

func Init(opts *Opts) http.Handler {
	service := NewServeModeService(&ServeServiceOpts{
		BaseDir: opts.BaseDir,
		Storage: opts.Storage,
	})
	controller := NewServeController(service)
	return initHandlers(controller)
}
