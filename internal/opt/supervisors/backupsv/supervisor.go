package backupsv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/robfig/cron/v3"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/core/logger"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/api/receivemode"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backup"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
	"github.com/pgrwl/pgrwl/internal/opt/shared/retry"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/cmdx"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/strx"
)

type BaseBackupSupervisorOpts struct {
	Directory string
}

type BaseBackupSupervisor struct {
	l             *slog.Logger
	cfg           *config.Config
	opts          *BaseBackupSupervisorOpts
	restyClient   *resty.Client
	backupRunning tryMutex
}

func NewBaseBackupSupervisor(cfg *config.Config, opts *BaseBackupSupervisorOpts) *BaseBackupSupervisor {
	client := resty.New()
	client.SetRetryCount(0)
	client.SetTimeout(5 * time.Second)

	return &BaseBackupSupervisor{
		l:           slog.With(slog.String("component", "basebackup-supervisor")),
		cfg:         cfg,
		opts:        opts,
		restyClient: client,
	}
}

func (u *BaseBackupSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}

	return slog.With(slog.String("component", "basebackup-supervisor"))
}

// Run starts the basebackup scheduler and blocks until ctx is canceled.
//
// Fatal/setup errors are returned:
//   - replication connection cannot be established
//   - startup info cannot be loaded
//   - cron expression is invalid
//
// Per-backup errors are logged and do not stop the scheduler:
//   - retention failed
//   - basebackup failed
//   - WAL cleanup failed
//   - panic inside a scheduled backup run
func (u *BaseBackupSupervisor) Run(ctx context.Context) error {
	startupInfo, err := u.loadStartupInfo(ctx)
	if err != nil {
		return err
	}

	// POSIX compatible cron syntax: "* * * * *".
	// No seconds field.
	c := cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))

	_, err = c.AddFunc(u.cfg.Backup.Cron, func() {
		if err := u.runScheduledBackupSafe(ctx, startupInfo); err != nil {
			if errors.Is(err, context.Canceled) {
				u.log().Info("scheduled basebackup stopped (context canceled)")
				return
			}
			u.log().Error("scheduled basebackup run failed", slog.Any("err", err))
		}
	})
	if err != nil {
		return fmt.Errorf("add basebackup cron job: %w", err)
	}

	c.Start()

	u.log().Info("basebackup scheduler started",
		slog.String("cron", u.cfg.Backup.Cron),
	)

	<-ctx.Done()

	u.log().Info("stopping basebackup scheduler")

	stopCtx := c.Stop()
	<-stopCtx.Done()

	u.log().Info("basebackup scheduler stopped")

	return ctx.Err()
}

func (u *BaseBackupSupervisor) loadStartupInfo(ctx context.Context) (*xlog.StartupInfo, error) {
	conn, err := retry.Do(ctx, retry.Policy{
		Delay: 5 * time.Second,
		Logger: u.log().With(
			slog.String("retry-operation", "connect-replication"),
		),
	}, func(ctx context.Context) (*pgconn.PgConn, error) {
		return pgconn.Connect(ctx, "application_name=pgrwl_basebackup replication=yes")
	})
	if err != nil {
		return nil, fmt.Errorf("connect replication: %w", err)
	}
	defer func() {
		_ = conn.Close(context.Background())
	}()

	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		return nil, fmt.Errorf("get startup info: %w", err)
	}

	return startupInfo, nil
}

func (u *BaseBackupSupervisor) runScheduledBackupSafe(
	ctx context.Context,
	startupInfo *xlog.StartupInfo,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scheduled basebackup panicked: %v", r)
		}
	}()

	return u.runScheduledBackup(ctx, startupInfo)
}

func (u *BaseBackupSupervisor) runScheduledBackup(
	ctx context.Context,
	startupInfo *xlog.StartupInfo,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !u.backupRunning.TryLock() {
		u.log().Warn("previous basebackup still running, skipping this run")
		return nil
	}
	defer func() {
		u.log().Debug("unlocking tryMutex")
		u.backupRunning.Unlock()
	}()

	u.log().Info("starting scheduled basebackup")

	if err := u.runRetention(ctx); err != nil {
		return fmt.Errorf("retain backups before basebackup: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	_, err := backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{
		Directory: u.opts.Directory,
	})
	if err != nil {
		return fmt.Errorf("create basebackup: %w", err)
	}

	u.log().Info("basebackup completed")

	if u.cfg.Backup.WalRetention.Enable {
		if err := u.cleanupWalArchive(ctx, startupInfo); err != nil {
			return fmt.Errorf("cleanup wal archive: %w", err)
		}
	}

	return nil
}

func (u *BaseBackupSupervisor) runRetention(ctx context.Context) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}

	switch u.cfg.Backup.Retention.Type {
	case config.BackupRetentionTypeTime:
		u.log().Info("starting retain backups", slog.String("type", "time-based"))

		if err := u.retainBackupsTimeBased(ctx, u.cfg); err != nil {
			return fmt.Errorf("time-based retention: %w", err)
		}

	case config.BackupRetentionTypeCount:
		u.log().Info("starting retain backups", slog.String("type", "count-based"))

		if err := u.retainBackupsCountBased(ctx, u.cfg); err != nil {
			return fmt.Errorf("count-based retention: %w", err)
		}

	default:
		return fmt.Errorf("unknown backup retention type: %q", u.cfg.Backup.Retention.Type)
	}

	return nil
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
		return fmt.Errorf("cannot use both: receiver.retention.enable && backup.wals.manage_cleanup")
	}

	addr, err := cmdx.Addr(u.cfg.Backup.WalRetention.ReceiverAddr)
	if err != nil {
		return err
	}

	// setup storage
	stor, err := api.SetupStorage(&api.SetupStorageOpts{
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
	info, err := u.readManifest(ctx, stor, oldest)
	if err != nil {
		return err
	}

	// get WAL filename as a starting point (to clean everything before that name)
	filename := xlog.XLogFileName(conv.ToUint32(info.TimelineID), uint64(info.StopLSN), startupInfo.WalSegSz)
	url := fmt.Sprintf("%s/api/v1/wal-before/%s", addr, filename)

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

func (u *BaseBackupSupervisor) readManifest(ctx context.Context, stor st.Storage, backupID string) (*backupdto.Result, error) {
	manifestFilename := filepath.Base(backupID) + ".json"

	manifestRdr, err := stor.Get(ctx, filepath.ToSlash(filepath.Join(filepath.Base(backupID), manifestFilename)))
	if err != nil {
		return nil, err
	}
	defer manifestRdr.Close()

	var info backupdto.Result
	if err := json.NewDecoder(manifestRdr).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

func (u *BaseBackupSupervisor) getReceiverConfig() (*receivemode.BriefConfig, error) {
	addr, err := cmdx.Addr(u.cfg.Backup.WalRetention.ReceiverAddr)
	if err != nil {
		return nil, err
	}

	var c receivemode.BriefConfig

	resp, err := u.restyClient.R().SetResult(&c).Get(addr + "/api/v1/brief-config")
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

	// setup storage, get backup IDs
	stor, backupTs, err := u.getBackupIDs(ctx, cfg)
	if err != nil {
		return err
	}
	if stor == nil || len(backupTs) == 0 {
		return nil
	}

	// decide which may be pruned
	backupsList := []string{}
	for k := range backupTs {
		backupsList = append(backupsList, filepath.Base(k))
	}

	backupsToDelete := filterBackupsToDeleteTimeBased(
		backupsList,
		cfg.Backup.Retention.KeepDurationParsed,
		time.Now(),
	)
	if len(backupsToDelete) == 0 {
		return nil
	}

	// purge
	return u.purgeBackups(ctx, backupTs, backupsToDelete, stor)
}

func (u *BaseBackupSupervisor) retainBackupsCountBased(ctx context.Context, cfg *config.Config) error {
	if !u.cfg.Backup.Retention.Enable {
		return nil
	}

	// setup storage, get backup IDs
	stor, backupTs, err := u.getBackupIDs(ctx, cfg)
	if err != nil {
		return err
	}
	if stor == nil || len(backupTs) == 0 {
		return nil
	}

	// decide which may be pruned
	backupsList := []string{}
	for k := range backupTs {
		backupsList = append(backupsList, filepath.Base(k))
	}

	backupsToDelete := filterBackupsToDeleteCountBased(
		backupsList,
		int(cfg.Backup.Retention.KeepCountParsed),
	)
	if len(backupsToDelete) == 0 {
		return nil
	}

	// purge
	return u.purgeBackups(ctx, backupTs, backupsToDelete, stor)
}

// internal helpers

func (u *BaseBackupSupervisor) purgeBackups(
	ctx context.Context,
	backupTs map[string]bool,
	backupsToDelete []string,
	stor st.Storage,
) error {
	// list backups in storage
	if config.Verbose {
		for k := range backupTs {
			u.log().LogAttrs(ctx, logger.LevelTrace, "backups in storage",
				slog.String("path", k),
			)
		}
	}

	// list backups to delete
	if config.Verbose {
		for _, k := range backupsToDelete {
			u.log().LogAttrs(ctx, logger.LevelTrace, "backups to delete",
				slog.String("path", k),
			)
		}
	}

	for b := range backupTs {
		for _, toDelete := range backupsToDelete {
			if filepath.Base(b) != filepath.Base(toDelete) {
				continue
			}

			// ONLY if manifest exists.
			// Failed backups may have no manifest, but should still be removable.
			info, readManifestErr := u.readManifest(ctx, stor, toDelete)

			if err := stor.DeleteDir(ctx, b); err != nil {
				return err
			}

			u.log().Info("backup retained", slog.String("path", b))

			// If backup was retained and manifest was present, update metrics.
			if readManifestErr == nil {
				u.log().Debug("bytes deleted", slog.Int64("total", info.BytesTotal))
				backupmetrics.M.AddBasebackupBytesDeleted(float64(info.BytesTotal))
			}
		}
	}

	return nil
}

func (u *BaseBackupSupervisor) getBackupIDs(ctx context.Context, cfg *config.Config) (st.Storage, map[string]bool, error) {
	// setup storage
	stor, err := api.SetupStorage(&api.SetupStorageOpts{
		BaseDir: filepath.ToSlash(cfg.Main.Directory),
		SubPath: config.BaseBackupSubpath,
	})
	if err != nil {
		return nil, nil, err
	}

	// get all backups available
	backupTs, err := stor.ListTopLevelDirs(ctx, "")
	if err != nil {
		return nil, nil, err
	}
	if len(backupTs) == 0 {
		return nil, nil, nil
	}

	if cfg.Backup.Retention.KeepLast != nil {
		if len(backupTs) <= *cfg.Backup.Retention.KeepLast {
			u.log().Debug("backup counts <= keep_last. nothing to purge")
			return nil, nil, nil
		}
	}

	return stor, backupTs, nil
}

// locks

type tryMutex struct {
	locked int32
	m      sync.Mutex
}

func (tm *tryMutex) TryLock() bool {
	if !atomic.CompareAndSwapInt32(&tm.locked, 0, 1) {
		return false
	}

	tm.m.Lock()
	return true
}

func (tm *tryMutex) Unlock() {
	atomic.StoreInt32(&tm.locked, 0)
	tm.m.Unlock()
}
