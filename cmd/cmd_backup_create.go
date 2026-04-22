package cmd

import (
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backup"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	_, err := backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{Directory: opts.Directory})
	return err
}
