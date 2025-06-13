package cmd

import (
	"github.com/hashmap-kz/pgrwl/internal/opt/basebackup"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	return basebackup.CreateBaseBackup(&basebackup.CreateBaseBackupOpts{Directory: opts.Directory})
}
