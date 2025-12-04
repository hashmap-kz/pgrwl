package restorecmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/fsx"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/strx"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/tarx"

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
	loggr.Info("querying backup files in storage")
	backupFiles, err := stor.List(ctx, backupID)
	if err != nil {
		return err
	}

	// construct restore info, prepare manifest
	loggr.Info("construct restore info")
	ri := makeRestoreInfo(backupID, backupFiles)
	mf, err := readManifestFile(ctx, backupID, stor, ri)
	if err != nil {
		return err
	}

	// preflight checks
	loggr.Info("running preflight checks")
	err = checkTblspcDirsEmpty(backupID, ri, mf)
	if err != nil {
		return err
	}

	// 1) restore base
	loggr.Info("restoring basebackup")
	rc, err := stor.Get(ctx, ri.BaseTar)
	if err != nil {
		return fmt.Errorf("get %s: %w", id, err)
	}
	if err := tarx.Untar(rc, dest); err != nil {
		return fmt.Errorf("untar %s: %w", id, err)
	}
	if err := rc.Close(); err != nil {
		return err
	}

	// 2) restore tablespaces
	loggr.Info("restoring tablespaces")
	err = restoreTblspc(ctx, backupID, dest, stor, ri, mf)
	if err != nil {
		return err
	}

	return nil
}
