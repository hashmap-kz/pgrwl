package sbackup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/receive/model"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/strx"

	"github.com/hashmap-kz/pgrwl/internal/core/conv"
	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/cmdx"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backup"

	"github.com/go-resty/resty/v2"
	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared"
	"github.com/robfig/cron/v3"
)

type BaseBackupSupervisorOpts struct {
	Directory string
	Verbose   bool
}

type BaseBackupSupervisor struct {
	l           *slog.Logger
	cfg         *config.Config
	opts        *BaseBackupSupervisorOpts
	verbose     bool
	restyClient *resty.Client

	// opts (for fast-access without traverse the config)
	storageName string
}

func NewBaseBackupSupervisor(cfg *config.Config, opts *BaseBackupSupervisorOpts) *BaseBackupSupervisor {
	client := resty.New()
	client.SetRetryCount(0)
	client.SetTimeout(5 * time.Second)
	return &BaseBackupSupervisor{
		l:           slog.With(slog.String("component", "basebackup-supervisor")),
		cfg:         cfg,
		opts:        opts,
		verbose:     opts.Verbose,
		restyClient: client,
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
	// get necessary info
	conn, err := pgconn.Connect(ctx, "application_name=pgrwl_basebackup replication=yes")
	if err != nil {
		u.log().Error("basebackup create-conn failed", slog.Any("err", err))
		return
	}
	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		u.log().Error("basebackup get-startup-info failed", slog.Any("err", err))
		return
	}

	// POSIX compatible cron syntax: "* * * * *". Without support of seconds.
	c := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))

	_, err = c.AddFunc(u.cfg.Backup.Cron, func() {
		u.log().Info("starting scheduled basebackup")
		// retain previous
		if u.cfg.Backup.Retention.Enable {
			if u.cfg.Backup.Retention.Type == config.BackupRetentionTypeTime {
				u.log().Info("starting retain backups (time-based)")
				if err := u.retainBackupsTimeBased(ctx, u.cfg); err != nil {
					u.log().Error("basebackup retain failed", slog.Any("err", err))
				}
			}
			if u.cfg.Backup.Retention.Type == config.BackupRetentionTypeCount {
				u.log().Info("starting retain backups (count-based)")
				if err := u.retainBackupsCountBased(ctx, u.cfg); err != nil {
					u.log().Error("basebackup retain failed", slog.Any("err", err))
				}
			}
		}
		// create backup
		_, err := backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{Directory: u.opts.Directory})
		if err != nil {
			u.log().Error("basebackup failed", slog.Any("err", err))
		} else {
			u.log().Info("basebackup completed")

			// cleanup wal-archive (when basebackup is completed without errors)
			if u.cfg.Backup.Wals.ManageCleanup {
				if err := u.cleanupWalArchive(ctx, startupInfo); err != nil {
					u.log().Error("wal-archive cleanup failed", slog.Any("err", err))
				}
			}
		}
	})
	if err != nil {
		u.log().Error("failed to add cron", slog.Any("err", err))
		os.Exit(1)
	}
	c.Start()
}

func (u *BaseBackupSupervisor) cleanupWalArchive(ctx context.Context, startupInfo *xlog.StartupInfo) error {
	u.log().Info("begin to cleanup wal-archive")

	// check receiver.config by API call
	// if receiver.retain.enabled we cannot cleanup archive here
	receiverConfig, err := u.getReceiverConfig()
	if err != nil {
		return err
	}

	if receiverConfig.RetentionEnable {
		return fmt.Errorf("cannot use both: (receiver.retention.enable && backup.wals.manage_cleanup)")
	}

	addr, err := cmdx.Addr(u.cfg.Backup.Wals.ReceiverAddr)
	if err != nil {
		return err
	}

	// setup storage
	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
		BaseDir: filepath.ToSlash(u.cfg.Main.Directory),
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

	// sort desc
	backupsSorted := strx.SortDesc(backupTs)
	oldest := backupsSorted[len(backupsSorted)-1]

	// get manifest
	manifestFilename := filepath.Base(oldest) + ".json"
	manifestRdr, err := stor.Get(ctx, filepath.ToSlash(filepath.Join(filepath.Base(oldest), manifestFilename)))
	if err != nil {
		return err
	}
	defer manifestRdr.Close()

	// unmarshal
	var info backup.Result
	if err := json.NewDecoder(manifestRdr).Decode(&info); err != nil {
		return err
	}

	filename := xlog.XLogFileName(conv.ToUint32(info.TimelineID), uint64(info.StopLSN), startupInfo.WalSegSz)
	url := fmt.Sprintf("%s/wal-before/%s", addr, filename)

	u.log().Info("cleanup data",
		slog.String("receiver-addr", addr),
		slog.String("url", url),
		slog.String("filename", filename),
	)

	resp, err := u.restyClient.R().Delete(url)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("cleanupWalArchive() request error: %d", resp.StatusCode())
	}
	return nil
}

func (u *BaseBackupSupervisor) getReceiverConfig() (*model.BriefConfig, error) {
	addr, err := cmdx.Addr(u.cfg.Backup.Wals.ReceiverAddr)
	if err != nil {
		return nil, err
	}

	var c model.BriefConfig
	resp, err := u.restyClient.R().SetResult(&c).Get(addr + "/config")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("getReceiverConfig() request error: %d", resp.StatusCode())
	}
	return &c, nil
}

func (u *BaseBackupSupervisor) retainBackupsTimeBased(ctx context.Context, cfg *config.Config) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}
	// setup storage
	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
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

	if cfg.Backup.Retention.KeepLast != nil {
		if len(backupTs) <= *cfg.Backup.Retention.KeepLast {
			u.log().Debug("backup counts <= keep_last. nothing to purge")
			return nil
		}
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
	backupsToDelete := filterBackupsToDeleteTimeBased(backupsList, cfg.Backup.Retention.KeepDurationParsed, time.Now())
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
				err := stor.DeleteDir(ctx, b)
				if err != nil {
					return err
				}
				u.log().Info("backup retained", slog.String("path", b))
			}
		}
	}

	return nil
}

func (u *BaseBackupSupervisor) retainBackupsCountBased(ctx context.Context, cfg *config.Config) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}
	// setup storage
	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
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

	if cfg.Backup.Retention.KeepLast != nil {
		if len(backupTs) <= *cfg.Backup.Retention.KeepLast {
			u.log().Debug("backup counts <= keep_last. nothing to purge")
			return nil
		}
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
	backupsToDelete := filterBackupsToDeleteCountBased(backupsList, int(cfg.Backup.Retention.KeepCountParsed))
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
				err := stor.DeleteDir(ctx, b)
				if err != nil {
					return err
				}
				u.log().Info("backup retained", slog.String("path", b))
			}
		}
	}

	return nil
}
