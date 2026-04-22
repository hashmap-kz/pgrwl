package receivemode

import (
	"net/http"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type Opts struct {
	PGRW     xlog.PgReceiveWal
	BaseDir  string
	Storage  *st.VariadicStorage
	JobQueue *jobq.JobQueue
	Cfg      *config.Config
}

func Init(opts *Opts) http.Handler {
	service := NewReceiveModeService(&ReceiveServiceOpts{
		PGRW:     opts.PGRW,
		BaseDir:  opts.BaseDir,
		Storage:  opts.Storage,
		JobQueue: opts.JobQueue,
	})
	controller := NewReceiveController(service)
	return initHandlers(opts.Cfg, controller)
}
