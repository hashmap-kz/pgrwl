package receivemode

import (
	"net/http"

	"github.com/pgrwl/pgrwl/internal/opt/api/middleware"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type Opts struct {
	PGRW    xlog.PgReceiveWal
	BaseDir string
	Storage *st.VariadicStorage
	Cfg     *config.Config
}

func Init(opts *Opts) http.Handler {
	service := NewReceiveModeService(&ReceiveServiceOpts{
		PGRW:    opts.PGRW,
		BaseDir: opts.BaseDir,
		Storage: opts.Storage,
	})
	controller := NewReceiveController(service)
	return middleware.Cors(initHandlers(opts.Cfg, controller))
}
