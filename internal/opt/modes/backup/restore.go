package backup

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/fsx"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/strx"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared"

	"github.com/hashmap-kz/pgrwl/config"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	loggr := slog.With(slog.String("component", "restore"), slog.String("id", id))

	// safe check
	// refusing to restore, if a target dir exists and it's not empty
	dirExistsAndNotEmpty, err := fsx.DirExistsAndNotEmpty(dest)
	if err != nil {
		return err
	}
	if dirExistsAndNotEmpty {
		return fmt.Errorf("refusing to restore in a non-empty dir: %s", dest)
	}

	loggr.Info("destination", slog.String("dest", filepath.ToSlash(dest)))
	if err := os.MkdirAll(filepath.ToSlash(dest), 0o750); err != nil {
		return err
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
	backupsTs, err := stor.ListTopLevelDirs(ctx, "")
	if err != nil {
		return err
	}

	// sort backup ids, decide which to use for restore
	iDsSortedDesc := strx.SortDesc(backupsTs)
	var backupID string
	if id != "" {
		if !strx.IsInList(id, iDsSortedDesc) {
			// given backup ID is not present in backups list
			return fmt.Errorf("no such backup: %s", id)
		}
		backupID = id
	} else {
		// a backup ID was not given, and there are no backups available, warn and return
		if len(iDsSortedDesc) == 0 {
			loggr.Warn("no backups in a storage")
			return nil
		}
		// get the 'latest' backup available
		backupID = iDsSortedDesc[0]
	}

	// get backup files for restore (*.tar)
	backupFiles, err := stor.List(ctx, backupID)
	if err != nil {
		return err
	}

	// TODO: tablespaces
	// untar archives
	for _, f := range backupFiles {
		loggr.Debug("restoring file", slog.String("path", filepath.ToSlash(f)))
		// skip internals
		if strings.Contains(f, backupID+".json") {
			continue
		}

		rc, err := stor.Get(ctx, f)
		if err != nil {
			return fmt.Errorf("get %s: %w", id, err)
		}
		// TODO: log() func
		if err := untar(rc, dest, loggr); err != nil {
			return fmt.Errorf("untar %s: %w", id, err)
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}
	return nil
}

func untar(r io.Reader, dest string, loggr *slog.Logger) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		//nolint:gosec
		target := filepath.ToSlash(filepath.Join(dest, hdr.Name))
		loggr.Debug("tar target", slog.String("path", target))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			//nolint:gosec
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			// skip other types (e.g., symlinks in tar)
		}
	}
	return nil
}
