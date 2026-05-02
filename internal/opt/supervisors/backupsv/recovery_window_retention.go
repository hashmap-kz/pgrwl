package backupsv

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pglogrepl"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type RecoveryWindowRetention interface {
	RunBeforeBackup(ctx context.Context) error
}

type recoveryWindowRetention struct {
	l           *slog.Logger
	cfg         *config.Config
	opts        *Opts
	walSegSz    uint64
	backupStore BackupStore
	walCleaner  WALCleaner
}

var _ RecoveryWindowRetention = &recoveryWindowRetention{}

func NewRecoveryWindowRetention(
	cfg *config.Config,
	opts *Opts,
	l *slog.Logger,
	basebackupStor st.Storage,
	walStor *st.VariadicStorage,
) RecoveryWindowRetention {
	if opts == nil {
		opts = &Opts{}
	}

	if l == nil {
		l = slog.With(slog.String("component", "recovery-window-retention"))
	}

	return &recoveryWindowRetention{
		l:           l,
		cfg:         cfg,
		opts:        opts,
		walSegSz:    opts.WalSegSz,
		backupStore: NewBackupStore(cfg, l, basebackupStor),
		walCleaner:  NewWALCleaner(opts, l, walStor),
	}
}

func (r *recoveryWindowRetention) RunBeforeBackup(ctx context.Context) error {
	r.l.Info("starting retention",
		slog.String("type", "recovery-window"),
		slog.Duration("recovery_window", r.cfg.Retention.KeepDurationParsed),
	)

	successful, err := r.loadSuccessfulBackups(ctx)
	if err != nil {
		return err
	}

	if len(successful) == 0 {
		r.l.Warn("recovery-window retention skipped: no successful backups with readable manifests")
		return nil
	}

	minimumBackups := 1
	if r.cfg.Retention.KeepLast != nil {
		minimumBackups = *r.cfg.Retention.KeepLast
	}

	anchor := chooseRecoveryWindowAnchor(
		successful,
		r.cfg.Retention.KeepDurationParsed,
		minimumBackups,
		time.Now().UTC(),
	)
	if anchor == nil {
		return nil
	}

	backupsToDelete := backupsOlderThanAnchor(successful, anchor)

	r.logPlan(anchor, backupsToDelete, len(successful), minimumBackups)

	if len(backupsToDelete) > 0 {
		if err := r.backupStore.DeleteBackups(ctx, backupsToDelete); err != nil {
			return fmt.Errorf("purge old backups: %w", err)
		}
	}

	if err := r.walCleaner.DeleteBefore(ctx, anchor.beginWAL); err != nil {
		return fmt.Errorf("purge old WALs before %s: %w", anchor.beginWAL, err)
	}

	return nil
}

func (r *recoveryWindowRetention) loadSuccessfulBackups(ctx context.Context) ([]recoveryWindowBackup, error) {
	backupDirs, err := r.backupStore.ListBackupDirs(ctx)
	if err != nil {
		return nil, err
	}
	if len(backupDirs) == 0 {
		return nil, nil
	}

	successful := make([]recoveryWindowBackup, 0, len(backupDirs))

	for backupPath := range backupDirs {
		backupID := backupBaseName(backupPath)

		info, err := r.backupStore.ReadManifest(ctx, backupID)
		if err != nil {
			r.l.Warn("backup skipped by retention because manifest cannot be read",
				slog.String("backup_id", backupID),
				slog.Any("err", err),
			)
			continue
		}

		if info.StartedAt.IsZero() {
			r.l.Warn("backup skipped by retention because started_at is empty",
				slog.String("backup_id", backupID),
			)
			continue
		}

		beginWAL := r.backupBeginWAL(info)
		if beginWAL == "" {
			r.l.Warn("backup skipped by retention because begin WAL cannot be determined",
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

	return successful, nil
}

func (r *recoveryWindowRetention) backupBeginWAL(info *backupdto.Result) string {
	if info == nil || r.walSegSz == 0 {
		return ""
	}

	timelineID := info.TimelineID
	startLSN := info.StartLSN

	if info.Manifest != nil {
		for _, walRange := range info.Manifest.WALRanges {
			if walRange.StartLSN == "" {
				continue
			}

			lsn, err := pglogrepl.ParseLSN(walRange.StartLSN)
			if err != nil || lsn == 0 {
				continue
			}

			startLSN = lsn

			if walRange.Timeline != 0 {
				timelineID = walRange.Timeline
			}

			break
		}
	}

	if timelineID == 0 || startLSN == 0 {
		return ""
	}

	segNo := xlog.XLByteToSeg(uint64(startLSN), r.walSegSz)

	return xlog.XLogFileName(
		conv.ToUint32(timelineID),
		segNo,
		r.walSegSz,
	)
}

func (r *recoveryWindowRetention) logPlan(
	anchor *recoveryWindowBackup,
	backupsToDelete []string,
	successfulBackups int,
	minimumBackups int,
) {
	r.l.Info("recovery-window retention plan",
		slog.String("anchor_backup", anchor.name),
		slog.String("keep_wal_from", anchor.beginWAL),
		slog.Duration("recovery_window", r.cfg.Retention.KeepDurationParsed),
		slog.Int("minimum_backups", minimumBackups),
		slog.Int("successful_backups", successfulBackups),
		slog.Int("delete_backups", len(backupsToDelete)),
	)
}
