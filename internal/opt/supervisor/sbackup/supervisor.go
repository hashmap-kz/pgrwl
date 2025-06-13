package sbackup

import (
	"context"
	"log/slog"
	"os"

	"github.com/hashmap-kz/pgrwl/internal/opt/basebackup"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/robfig/cron/v3"
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

func (u *BaseBackupSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}
	return slog.With(slog.String("component", "basebackup-supervisor"))
}

func (u *BaseBackupSupervisor) Run(ctx context.Context, _ *jobq.JobQueue) {
	c := cron.New(cron.WithSeconds()) // enables support for seconds (optional)

	// example: "0 * * * * *"

	_, err := c.AddFunc(u.cfg.Backup.Cron, func() {
		u.log().Info("starting scheduled basebackup")
		// create backup
		err := basebackup.Run(&basebackup.CmdOpts{Directory: u.opts.Directory})
		if err != nil {
			u.log().Error("basebackup failed", slog.Any("err", err))
		} else {
			u.log().Info("basebackup completed")
		}
		// retain previous
		if u.cfg.Backup.Retention.Enable {
			u.log().Info("starting retain backups")
			if err := u.retainBackups(ctx); err != nil {
				u.log().Error("basebackup retain failed", slog.Any("err", err))
			}
		}
	})
	if err != nil {
		u.log().Error("failed to add cron", slog.Any("err", err))
		os.Exit(1)
	}
	c.Start()
}

//nolint:unparam
func (u *BaseBackupSupervisor) retainBackups(_ context.Context) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}
	return nil
}
