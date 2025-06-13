package sbackup

import (
	"context"
	"log/slog"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	"github.com/hashmap-kz/pgrwl/config"
)

type BaseBackupSupervisorOpts struct {
	Directory string
	Verbose   bool
}

type BaseBackupSupervisor struct {
	l       *slog.Logger
	cfg     *config.Config
	opts    *BaseBackupSupervisorOpts
	verbose bool

	// opts (for fast-access without traverse the config)
	storageName string
}

func NewBaseBackupSupervisor(cfg *config.Config, opts *BaseBackupSupervisorOpts) *BaseBackupSupervisor {
	return &BaseBackupSupervisor{
		l:           slog.With(slog.String("component", "basebackup-supervisor")),
		cfg:         cfg,
		opts:        opts,
		verbose:     opts.Verbose,
		storageName: cfg.Storage.Name,
	}
}

//nolint:unused
func (u *BaseBackupSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}
	return slog.With(slog.String("component", "basebackup-supervisor"))
}

func (u *BaseBackupSupervisor) Run(_ context.Context, _ *jobq.JobQueue) {
}
