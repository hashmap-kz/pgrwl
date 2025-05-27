package supervisor

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"

	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func (u *ArchiveSupervisor) filterOlderThan(files []storage.FileInfo, maxAge time.Duration) []storage.FileInfo {
	var result []storage.FileInfo
	cutoff := time.Now().Add(-maxAge)
	for _, f := range files {
		if f.ModTime.Before(cutoff) {
			result = append(result, f)
		}
	}
	return result
}

func (u *ArchiveSupervisor) performRetention(ctx context.Context, daysKeepRetention time.Duration) error {
	fileInfos, err := u.stor.ListInfo(ctx, "")
	if err != nil {
		return err
	}
	if len(fileInfos) == 0 {
		return nil
	}

	olderThan := u.filterOlderThan(fileInfos, daysKeepRetention)
	if len(olderThan) == 0 {
		return nil
	}

	// TODO: bulk delete, no iterations
	u.log().Debug("begin to retain files", slog.Int("cnt", len(olderThan)))
	for _, elem := range olderThan {
		u.log().Debug("delete file", slog.String("path", filepath.ToSlash(elem.Path)))
		err := u.stor.Delete(ctx, elem.Path)
		if err != nil {
			return err
		}
	}

	metrics.PgrwlMetricsCollector.IncWALFilesDeleted(u.storageName)
	return nil
}
