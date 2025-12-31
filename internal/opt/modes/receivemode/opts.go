package receivemode

import (
	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/wrk"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

type ReceiveDaemonRunOpts struct {
	PGRW               xlog.PgReceiveWal
	BaseDir            string
	Verbose            bool
	Storage            *storage.VariadicStorage
	JobQueue           *jobq.JobQueue // optional, nil in 'serve' mode
	ReceiverController *wrk.WorkerController
	ArchiveController  *wrk.WorkerController
}
