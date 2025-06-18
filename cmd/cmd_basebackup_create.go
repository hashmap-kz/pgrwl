package cmd

import (
	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backup"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	_, err := backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{Directory: opts.Directory})
	return err
}
