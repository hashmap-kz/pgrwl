package sbackup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/hashmap-kz/pgrwl/internal/opt/supervisor"

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
			if err := u.retainBackups(ctx, u.cfg); err != nil {
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

func (u *BaseBackupSupervisor) retainBackups(ctx context.Context, cfg *config.Config) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}
	// setup storage
	stor, err := supervisor.SetupStorage(&supervisor.SetupStorageOpts{
		BaseDir: filepath.ToSlash(cfg.Main.Directory),
		SubPath: config.BaseBackupSubpath,
	})
	if err != nil {
		return err
	}

	// get all backups available
	backupTs, err := stor.ListTopLevelDirs(ctx, "")
	if err != nil {
		return err
	}
	if len(backupTs) == 0 {
		return nil
	}

	// list backups in storage
	if u.verbose {
		for k := range backupTs {
			u.log().LogAttrs(ctx, logger.LevelTrace, "backups in storage",
				slog.String("path", k),
			)
		}
	}

	// decide which may be pruned
	backupsList := []string{}
	for k := range backupTs {
		backupsList = append(backupsList, filepath.Base(k))
	}
	backupsToDelete := filterBackupsToDelete(backupsList, cfg.Backup.Retention.KeepPeriodParsed, time.Now())
	if len(backupsToDelete) == 0 {
		return nil
	}

	// list backups to delete
	if u.verbose {
		for _, k := range backupsToDelete {
			u.log().LogAttrs(ctx, logger.LevelTrace, "backups to delete",
				slog.String("path", k),
			)
		}
	}

	// purge
	for b := range backupTs {
		for _, toDelete := range backupsToDelete {
			if filepath.Base(b) == filepath.Base(toDelete) {
				err := stor.DeleteAll(ctx, b)
				if err != nil {
					return err
				}
				u.log().Info("backup retained", slog.String("path", b))
			}
		}
	}

	return nil
}
