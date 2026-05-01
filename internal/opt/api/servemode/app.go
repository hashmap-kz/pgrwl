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
	service := NewServeModeService(opts)
	handler := NewHandler(service)
	return initHandlers(handler)
}
