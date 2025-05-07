package httpsrv

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// TODO: stat: skipped by permission issues, or vanished

func DirSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				slog.Warn("permission denied, skipping",
					slog.String("job", "dir-size-walk"),
					slog.String("path", filepath.Join(path, d.Name())),
				)
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil // skip subdirectory metadata
		}
		info, err := d.Info() // calls os.Lstat() once
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				slog.Warn("not exist, skipping",
					slog.String("job", "dir-size-walk"),
					slog.String("path", filepath.Join(path, d.Name())),
				)
				return nil
			}
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func ByteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}
