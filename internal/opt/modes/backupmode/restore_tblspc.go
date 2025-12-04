package backupmode

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/fsx"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func getTblspcLocation(tarName string, mf *Result) (Tablespace, error) {
	// storage:
	//
	// 0 = {string} "20251203150245/20251203150245.json"
	// 1 = {string} "20251203150245/25222.tar"
	// 2 = {string} "20251203150245/base.tar"

	// manifest:
	//
	// {
	//   "start_lsn": 40047214632,
	//   "stop_lsn": 40047214880,
	//   "timeline_id": 1,
	//   "tablespaces": [
	//     {
	//       "oid": 25222,
	//       "location": "/mnt/pg_tablespaces/my_data_tablespace"
	//     }
	//   ],
	//   "bytes_total": 46151680
	// }

	// filesystem:
	//
	// root@deb:/mnt/894.3G/postgresql/pg_tblspc# ls -lah
	// total 8.0K
	// drwx------  2 postgres postgres 4.0K Nov 27 22:13 .
	// drwx------ 19 postgres postgres 4.0K Dec  3 18:52 ..
	// lrwxrwxrwx  1 postgres postgres   38 Nov 27 22:13 25222 -> /mnt/pg_tablespaces/my_data_tablespace
	//
	// root@deb:/mnt/894.3G/postgresql/pg_tblspc# ls -lah /mnt/pg_tablespaces/my_data_tablespace
	// total 12K
	// drwx------ 3 postgres postgres 4.0K Nov 27 22:13 .
	// drwxr-xr-x 3 root     root     4.0K Nov 27 22:13 ..
	// drwx------ 3 postgres postgres 4.0K Dec  3 19:40 PG_17_202406281

	if len(mf.Tablespaces) == 0 {
		return Tablespace{}, fmt.Errorf("tablespaces map is empty")
	}
	for _, ts := range mf.Tablespaces {
		// tarName -> "20251203150245/25222.tar"
		// ts.OID  -> 25222
		if strings.HasPrefix(filepath.Base(tarName), fmt.Sprintf("%d", ts.OID)) {
			return ts, nil
		}
	}
	return Tablespace{}, fmt.Errorf("cannot find tablespace target location: %s", tarName)
}

func checkTblspcDirsEmpty(
	id string,
	ri *RestoreInfo,
	mf *Result,
) error {
	loggr := slog.With(slog.String("component", "restore"), slog.String("id", id))
	if len(ri.TablespacesTars) == 0 {
		loggr.Debug("no tablespaces to restore, skipping")
		return nil
	}

	// safe check
	// refusing to restore, if a target dir exists and it's not empty
	for _, f := range ri.TablespacesTars {
		tsInfo, err := getTblspcLocation(f, mf)
		if err != nil {
			return err
		}

		dest := tsInfo.Location

		loggr.Debug("check that target dir for restoring tblspc is empty", slog.String("path", dest))
		dirExistsAndNotEmpty, err := fsx.DirExistsAndNotEmpty(dest)
		if err != nil {
			return err
		}
		if dirExistsAndNotEmpty {
			return fmt.Errorf("refusing to restore in a non-empty dir: %s", dest)
		}
	}
	return nil
}

func restoreTblspc(
	ctx context.Context,
	id, pgdata string,
	stor storage.Storage,
	ri *RestoreInfo,
	mf *Result,
) error {
	loggr := slog.With(slog.String("component", "restore"), slog.String("id", id))

	if len(ri.TablespacesTars) == 0 {
		return nil
	}

	for _, f := range ri.TablespacesTars {
		rc, err := stor.Get(ctx, f)
		if err != nil {
			return fmt.Errorf("get %s: %w", id, err)
		}
		tsInfo, err := getTblspcLocation(f, mf)
		if err != nil {
			return err
		}

		dest := tsInfo.Location

		loggr.Info("tblspc restore dest", slog.String("path", dest))
		if err := untar(rc, dest); err != nil {
			return fmt.Errorf("untar %s: %w", id, err)
		}
		if err := rc.Close(); err != nil {
			return err
		}

		tsInPgdata := filepath.Join(pgdata, "pg_tblspc", fmt.Sprintf("%d", tsInfo.OID))
		if err := fsx.EnsureSymlink(dest, tsInPgdata); err != nil {
			return err
		}
	}

	return nil
}
