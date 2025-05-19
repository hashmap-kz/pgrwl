package loops

import (
	"context"
	"log"
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
				slog.Info("uploader-loop, handle file", slog.String("path", path))

				file, err := os.Open(path)
				if err != nil {
					slog.Error("error open file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
					continue
				}
				err = stor.Put(ctx, entry.Name(), file)
				if err != nil {
					slog.Error("error upload file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)

					// upload error: closing file, continue the loop, DO NOT REMOVE SOURCE WHEN UPLOAD IS FAILED
					err = file.Close()
					if err != nil {
						slog.Error("error close file",
							slog.String("component", "uploader-loop"),
							slog.String("path", path),
							slog.Any("err", err),
						)
					}
					continue
				}

				// upload success: closing file
				err = file.Close()
				if err != nil {
					slog.Error("error close file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
				}

				if err := os.Remove(path); err != nil {
					log.Printf("delete failed: %s: %v", path, err)
					slog.Error("delete failed",
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
