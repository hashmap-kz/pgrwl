package cmd

import (
	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backup"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	return backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{Directory: opts.Directory})
}
