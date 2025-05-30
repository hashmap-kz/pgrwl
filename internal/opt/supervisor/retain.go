package supervisor

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"

	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func (u *ArchiveSupervisor) filterOlderThan(
	files []storage.FileInfo,
	cutoff time.Time,
) []string {
	var result []string
	for _, f := range files {
		if f.ModTime.UTC().Before(cutoff) {
			result = append(result, filepath.ToSlash(f.Path))
		}
	}
	slices.Sort(result)
	return result
}

func (u *ArchiveSupervisor) performRetention(ctx context.Context, keepPeriod time.Duration) error {
	cutoff := time.Now().UTC().Add(-keepPeriod)
	u.log().Debug("retention cutoff", slog.String("cutoff", cutoff.Format(time.DateTime)))

	fileInfos, err := u.stor.ListInfo(ctx, "")
	if err != nil {
		return err
	}
	if len(fileInfos) == 0 {
		return nil
	}

	if u.verbose {
		for i := range fileInfos {
			u.log().LogAttrs(ctx, logger.LevelTrace, "files in storage",
				slog.String("modtime", fileInfos[i].ModTime.Format(time.DateTime)),
				slog.String("cutoff", cutoff.Format(time.DateTime)),
				slog.String("path", fileInfos[i].Path),
			)
		}
	}

	olderThan := u.filterOlderThan(fileInfos, cutoff)
	if len(olderThan) == 0 {
		return nil
	}

	if u.verbose {
		for i := range olderThan {
			u.log().LogAttrs(ctx, logger.LevelTrace, "files to retain",
				slog.String("path", olderThan[i]),
			)
		}
	}

	u.log().Debug("begin to retain files", slog.Int("cnt", len(olderThan)))
	if err := u.stor.DeleteAllBulk(ctx, olderThan); err != nil {
		return err
	}

	metrics.PgrwlMetricsCollector.AddWALFilesDeleted(float64(len(olderThan)))
	return nil
}
