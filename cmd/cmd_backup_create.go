package cmd

import (
	"github.com/pgrwl/pgrwl/internal/opt/basebackup"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	_, err := basebackup.CreateBaseBackup(&basebackup.CreateBaseBackupOpts{Directory: opts.Directory})
	return err
}
