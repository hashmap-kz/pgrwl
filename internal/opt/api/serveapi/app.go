package serveapi

import (
	"net/http"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type Opts struct {
	BaseDir string
	Storage *st.VariadicStorage
}

func Init(opts *Opts) http.Handler {
	handler := NewHandler(NewService(opts))
	return initHandlers(handler)
}
