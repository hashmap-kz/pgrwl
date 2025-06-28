package cmd

import (
	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backupmode"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	_, err := backupmode.CreateBaseBackup(&backupmode.CreateBaseBackupOpts{Directory: opts.Directory})
	return err
}
