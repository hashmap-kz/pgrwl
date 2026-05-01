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

	"github.com/jackc/pglogrepl"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/robfig/cron/v3"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/core/logger"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backup"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
	"github.com/pgrwl/pgrwl/internal/opt/shared/retry"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

var ErrBackupAlreadyRunning = errors.New("basebackup is already running")

type BackupRunStatus string

const (
	BackupRunIdle      BackupRunStatus = "idle"
	BackupRunRunning   BackupRunStatus = "running"
	BackupRunSucceeded BackupRunStatus = "succeeded"
	BackupRunFailed    BackupRunStatus = "failed"
)

type BackupRunState struct {
	Running    bool            `json:"running"`
	Status     BackupRunStatus `json:"status"`
	Source     string          `json:"source,omitempty"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
}

type BaseBackupSupervisorOpts struct {
	Directory string
}

type BaseBackupSupervisor struct {
	l             *slog.Logger
	cfg           *config.Config
	opts          *BaseBackupSupervisorOpts
	backupRunning tryMutex

	stateMu sync.RWMutex
	state   BackupRunState
}

func NewBaseBackupSupervisor(cfg *config.Config, opts *BaseBackupSupervisorOpts) *BaseBackupSupervisor {
	return &BaseBackupSupervisor{
		l:    slog.With(slog.String("component", "basebackup-supervisor")),
		cfg:  cfg,
		opts: opts,
		state: BackupRunState{
			Running: false,
			Status:  BackupRunIdle,
		},
	}
}

func (u *BaseBackupSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}

	return slog.With(slog.String("component", "basebackup-supervisor"))
}

// TryBeginBackup atomically reserves the single backup slot.
//
// Both cron and manual REST-triggered backups must call this method before
// doing any backup work. This avoids a check-then-start race.
func (u *BaseBackupSupervisor) TryBeginBackup(source string) bool {
	if !u.backupRunning.TryLock() {
		return false
	}

	now := time.Now().UTC()

	u.stateMu.Lock()
	u.state = BackupRunState{
		Running:    true,
		Status:     BackupRunRunning,
		Source:     source,
		StartedAt:  &now,
		FinishedAt: nil,
		LastError:  "",
	}
	u.stateMu.Unlock()

	return true
}

// FinishBackup releases the single backup slot and stores the final state.
// It must be called exactly once after a successful TryBeginBackup().
func (u *BaseBackupSupervisor) FinishBackup(status BackupRunStatus, errMsg string) {
	now := time.Now().UTC()

	u.stateMu.Lock()
	u.state.Running = false
	u.state.Status = status
	u.state.FinishedAt = &now
	u.state.LastError = errMsg
	u.stateMu.Unlock()

	u.backupRunning.Unlock()
}

func (u *BaseBackupSupervisor) BackupStatus() BackupRunState {
	u.stateMu.RLock()
	defer u.stateMu.RUnlock()

	return cloneBackupRunState(u.state)
}

func cloneBackupRunState(in BackupRunState) BackupRunState {
	out := in

	if in.StartedAt != nil {
		t := *in.StartedAt
		out.StartedAt = &t
	}

	if in.FinishedAt != nil {
		t := *in.FinishedAt
		out.FinishedAt = &t
	}

	return out
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
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				u.log().Info("scheduled basebackup stopped", slog.Any("reason", err))
				return
			}

			if errors.Is(err, ErrBackupAlreadyRunning) {
				u.log().Warn("previous basebackup still running, skipping this run")
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
) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !u.TryBeginBackup("cron") {
		return ErrBackupAlreadyRunning
	}

	defer func() {
		if err != nil {
			u.FinishBackup(BackupRunFailed, err.Error())
			return
		}

		u.FinishBackup(BackupRunSucceeded, "")
	}()

	u.log().Info("starting scheduled basebackup")

	if err := u.runRetention(ctx, startupInfo); err != nil {
		return fmt.Errorf("retention before basebackup: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	_, err = backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{
		Directory: u.opts.Directory,
	})
	if err != nil {
		return fmt.Errorf("create basebackup: %w", err)
	}

	u.log().Info("basebackup completed")

	return nil
}

func (u *BaseBackupSupervisor) runRetention(
	ctx context.Context,
	startupInfo *xlog.StartupInfo,
) error {
	if !u.cfg.Retention.Enable {
		return nil
	}

	if u.cfg.Retention.Type != config.RetentionTypeRecoveryWindow {
		return fmt.Errorf(
			"unsupported backup retention type %q: only %q is supported",
			u.cfg.Retention.Type,
			config.RetentionTypeRecoveryWindow,
		)
	}

	u.log().Info("starting retention",
		slog.String("type", "recovery-window"),
		slog.Duration("recovery_window", u.cfg.Retention.KeepDurationParsed),
	)

	return u.retainRecoveryWindow(ctx, startupInfo, u.cfg)
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

func (u *BaseBackupSupervisor) retainRecoveryWindow(
	ctx context.Context,
	startupInfo *xlog.StartupInfo,
	cfg *config.Config,
) error {
	backupStore, backupDirs, err := u.getBackupIDs(ctx, cfg)
	if err != nil {
		return err
	}
	if backupStore == nil || len(backupDirs) == 0 {
		return nil
	}

	successful := make([]recoveryWindowBackup, 0, len(backupDirs))

	for backupPath := range backupDirs {
		backupID := filepath.Base(backupPath)

		info, err := u.readManifest(ctx, backupStore, backupID)
		if err != nil {
			u.log().Warn("backup skipped by retention because manifest cannot be read",
				slog.String("backup_id", backupID),
				slog.Any("err", err),
			)
			continue
		}

		if info.StartedAt.IsZero() {
			u.log().Warn("backup skipped by retention because started_at is empty",
				slog.String("backup_id", backupID),
			)
			continue
		}

		beginWAL := u.backupBeginWAL(info, startupInfo)
		if beginWAL == "" {
			u.log().Warn("backup skipped by retention because begin WAL cannot be determined",
				slog.String("backup_id", backupID),
				slog.String("start_lsn", info.StartLSN.String()),
				slog.Int("timeline_id", int(info.TimelineID)),
			)
			continue
		}

		successful = append(successful, recoveryWindowBackup{
			name:      backupID,
			path:      backupPath,
			startedAt: info.StartedAt,
			beginWAL:  beginWAL,
		})
	}

	if len(successful) == 0 {
		u.log().Warn("recovery-window retention skipped: no successful backups with readable manifests")
		return nil
	}

	minimumBackups := 1
	if cfg.Retention.KeepLast != nil {
		minimumBackups = *cfg.Retention.KeepLast
	}

	anchor := chooseRecoveryWindowAnchor(
		successful,
		cfg.Retention.KeepDurationParsed,
		minimumBackups,
		time.Now().UTC(),
	)
	if anchor == nil {
		return nil
	}

	backupsToDelete := backupsOlderThanAnchor(successful, anchor)

	u.log().Info("recovery-window retention plan",
		slog.String("anchor_backup", anchor.name),
		slog.String("keep_wal_from", anchor.beginWAL),
		slog.Duration("recovery_window", cfg.Retention.KeepDurationParsed),
		slog.Int("minimum_backups", minimumBackups),
		slog.Int("successful_backups", len(successful)),
		slog.Int("delete_backups", len(backupsToDelete)),
	)

	if len(backupsToDelete) > 0 {
		if err := u.purgeBackups(ctx, backupDirs, backupsToDelete, backupStore); err != nil {
			return fmt.Errorf("purge old backups: %w", err)
		}
	}

	if err := u.purgeWALsBefore(ctx, anchor.beginWAL); err != nil {
		return fmt.Errorf("purge old WALs before %s: %w", anchor.beginWAL, err)
	}

	return nil
}

func (u *BaseBackupSupervisor) backupBeginWAL(
	info *backupdto.Result,
	startupInfo *xlog.StartupInfo,
) string {
	if info == nil || startupInfo == nil {
		return ""
	}

	timelineID := info.TimelineID
	startLSN := info.StartLSN

	if info.Manifest != nil && len(info.Manifest.WALRanges) > 0 {
		firstRange := info.Manifest.WALRanges[0]

		if firstRange.Timeline != 0 {
			timelineID = firstRange.Timeline
		}

		if firstRange.StartLSN != "" {
			lsn, err := pglogrepl.ParseLSN(firstRange.StartLSN)
			if err == nil {
				startLSN = lsn
			}
		}
	}

	if timelineID == 0 || startLSN == 0 {
		return ""
	}

	return xlog.XLogFileName(
		conv.ToUint32(timelineID),
		uint64(startLSN),
		startupInfo.WalSegSz,
	)
}

func (u *BaseBackupSupervisor) purgeWALsBefore(ctx context.Context, keepFromWAL string) error {
	if keepFromWAL == "" {
		return fmt.Errorf("keepFromWAL is empty")
	}

	walStore, err := u.setupWALArchiveStorage()
	if err != nil {
		return err
	}

	wals, err := walStore.ListInfoRaw(ctx, "")
	if err != nil {
		return fmt.Errorf("list WAL archive: %w", err)
	}

	deleted := 0
	kept := 0

	for _, wal := range wals {
		name, history, ok := normalizeWALFilename(wal.Path)
		if !ok {
			kept++
			continue
		}

		// Timeline history files are tiny and important for timeline switching.
		// Keep them for now.
		if history {
			kept++
			continue
		}

		if !walBefore(name, keepFromWAL) {
			kept++
			continue
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		u.log().Info("deleting old WAL",
			slog.String("wal", name),
			slog.String("path", wal.Path),
			slog.String("keep_from", keepFromWAL),
		)

		if err := walStore.Delete(ctx, wal.Path); err != nil {
			return fmt.Errorf("delete WAL %s: %w", wal.Path, err)
		}

		deleted++
	}

	u.log().Info("WAL retention completed",
		slog.String("keep_from", keepFromWAL),
		slog.Int("deleted_wals", deleted),
		slog.Int("kept_wals", kept),
	)

	return nil
}

func (u *BaseBackupSupervisor) setupWALArchiveStorage() (*st.VariadicStorage, error) {
	// This storage points to the WAL archive, not basebackup storage.
	// Do not use basebackup storage here.
	// Do not pass S3PartSizeBytes here; retention only lists/deletes.
	return api.SetupStorage(&api.SetupStorageOpts{
		BaseDir: filepath.ToSlash(u.opts.Directory),
		SubPath: config.LocalFSStorageSubpath,
	})
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
