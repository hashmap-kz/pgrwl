package loops

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func RunUploaderLoop(ctx context.Context, stor storage.Storage, dir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("(uploader-loop) context is done, exiting...")
			return
		case <-ticker.C:
			files, err := os.ReadDir(dir)
			if err != nil {
				slog.Error("error reading dir",
					slog.String("component", "uploader-loop"),
					slog.Any("err", err),
				)
				continue
			}

			for _, entry := range files {
				if entry.IsDir() {
					continue
				}
				if !xlog.IsXLogFileName(entry.Name()) {
					continue
				}

				path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
				err = uploadOneFile(ctx, stor, path)
				if err != nil {
					slog.Error("error upload file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
				} else {
					slog.Info("uploaded and deleted",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
					)
				}
			}
		}
	}
}

func uploadOneFile(ctx context.Context, stor storage.Storage, path string) error {
	slog.Info("uploader-loop, handle file", slog.String("path", path))

	file, err := os.Open(path)
	if err != nil {
		return err
	}

	err = stor.Put(ctx, filepath.Base(path), file)
	if err != nil {
		// upload error: close the file, return err, DO NOT REMOVE SOURCE WHEN UPLOAD IS FAILED
		_ = file.Close()
		return err
	}

	// upload success: closing file
	if err := file.Close(); err != nil {
		return err
	}

	// remove file
	if err := os.Remove(path); err != nil {
		return err
	}

	return nil
}
