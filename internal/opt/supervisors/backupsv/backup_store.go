package backupsv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/logger"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
)

type BackupStore interface {
	ListBackupDirs(ctx context.Context) (map[string]bool, error)
	ReadManifest(
		ctx context.Context,
		backupID string,
	) (*backupdto.Result, error)
	DeleteBackups(
		ctx context.Context,
		backupsToDelete []string,
	) error
}

type backupStore struct {
	l    *slog.Logger
	opts *BackupSupervisorOpts
}

var _ BackupStore = &backupStore{}

func NewBackupStore(opts *BackupSupervisorOpts) BackupStore {
	return &backupStore{
		l:    slog.With(slog.String("component", "backup-store")),
		opts: opts,
	}
}

func (s *backupStore) ListBackupDirs(ctx context.Context) (map[string]bool, error) {
	backupDirs, err := s.opts.BasebackupStor.ListTopLevelDirs(ctx, "")
	if err != nil {
		return nil, err
	}

	if len(backupDirs) == 0 {
		return nil, nil
	}

	return backupDirs, nil
}

func (s *backupStore) ReadManifest(
	ctx context.Context,
	backupID string,
) (*backupdto.Result, error) {
	manifestFilename := backupBaseName(backupID) + ".json"

	manifestPath := filepath.ToSlash(filepath.Join(
		backupBaseName(backupID),
		manifestFilename,
	))

	manifestRdr, err := s.opts.BasebackupStor.Get(ctx, manifestPath)
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

func (s *backupStore) DeleteBackups(
	ctx context.Context,
	backupsToDelete []string,
) error {
	if len(backupsToDelete) == 0 {
		return nil
	}

	stor := s.opts.BasebackupStor

	backupDirs, err := stor.ListTopLevelDirs(ctx, "")
	if err != nil {
		return err
	}

	if config.Verbose {
		for path := range backupDirs {
			s.l.LogAttrs(ctx, logger.LevelTrace, "backups in storage",
				slog.String("path", path),
			)
		}

		for _, path := range backupsToDelete {
			s.l.LogAttrs(ctx, logger.LevelTrace, "backups to delete",
				slog.String("path", path),
			)
		}
	}

	deleteSet := make(map[string]struct{}, len(backupsToDelete))
	for _, backupID := range backupsToDelete {
		deleteSet[backupBaseName(backupID)] = struct{}{}
	}

	for backupPath := range backupDirs {
		backupID := backupBaseName(backupPath)

		if _, ok := deleteSet[backupID]; !ok {
			continue
		}

		info, readManifestErr := s.ReadManifest(ctx, backupID)

		if err := stor.DeleteDir(ctx, backupPath); err != nil {
			return fmt.Errorf("delete backup %s: %w", backupPath, err)
		}

		s.l.Info("backup deleted",
			slog.String("path", backupPath),
		)

		if readManifestErr == nil {
			s.l.Debug("backup bytes deleted",
				slog.Int64("total", info.BytesTotal),
			)
			backupmetrics.M.AddBasebackupBytesDeleted(float64(info.BytesTotal))
		}
	}

	return nil
}

func backupBaseName(path string) string {
	return filepath.Base(path)
}
