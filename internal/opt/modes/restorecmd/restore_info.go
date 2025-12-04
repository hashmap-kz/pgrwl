package restorecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/dto/backupdto"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func makeRestoreInfo(backupID string, backupFiles []string) *backupdto.RestoreInfo {
	loggr := slog.With(slog.String("component", "restore"), slog.String("id", backupID))
	r := backupdto.RestoreInfo{}

	// 0 = {string} "20251203150245/20251203150245.json"
	// 1 = {string} "20251203150245/25222.tar"
	// 2 = {string} "20251203150245/base.tar"

	for _, fname := range backupFiles {
		// slight cleanup of path for querying
		tmp := filepath.ToSlash(fname)
		tmp = strings.TrimPrefix(tmp, backupID+"/")

		// check that files we have
		isManifest := strings.HasPrefix(tmp, backupID+".json")
		correctFile := isManifest || strings.HasSuffix(tmp, ".tar")
		if !correctFile {
			loggr.Warn("skipping unknown type of file", slog.String("name", fname))
			continue
		}

		// build result
		if isManifest {
			r.ManifestFile = fname
		} else if strings.HasPrefix(tmp, "base.") {
			r.BaseTar = fname
		} else {
			r.TablespacesTars = append(r.TablespacesTars, fname)
		}
	}
	return &r
}

func readManifestFile(
	ctx context.Context,
	backupID string,
	stor storage.Storage,
	ri *backupdto.RestoreInfo,
) (*backupdto.Result, error) {
	if ri.ManifestFile == "" {
		return nil, fmt.Errorf("no manifest file (*%s.json*) found for backup %s", backupID+".json", backupID)
	}
	mrc, err := stor.Get(ctx, ri.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("get manifest %s: %w", ri.ManifestFile, err)
	}
	var mf backupdto.Result
	if err := json.NewDecoder(mrc).Decode(&mf); err != nil {
		mrc.Close()
		return nil, fmt.Errorf("decode manifest %s: %w", ri.ManifestFile, err)
	}
	if err := mrc.Close(); err != nil {
		return nil, err
	}
	return &mf, nil
}
