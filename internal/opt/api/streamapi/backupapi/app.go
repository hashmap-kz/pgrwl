package backupapi

import (
	"context"

	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"
)

type Opts struct {
	Supervisor backupsv.BaseBackupSupervisor
	AppCtx     context.Context
}
