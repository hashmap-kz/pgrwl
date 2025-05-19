package loops

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashmap-kz/pgrwl/cmd/cmdutils"
	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

func RunUploaderLoop(ctx context.Context, cfg *config.Config, stor storage.Storage, dir string) {
	syncInterval := cmdutils.ParseDurationOrDefault(cfg.Storage.Upload.SyncInterval, 30*time.Second)

	ticker := time.NewTicker(syncInterval)
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

			filesToUpload := filterFilesToUpload(dir, files)
			if len(filesToUpload) == 0 {
				continue
			}
			err = uploadFiles(ctx, cfg, stor, filesToUpload)
			if err != nil {
				slog.Error("error upload files",
					slog.String("component", "uploader-loop"),
					slog.Any("err", err),
				)
			}
		}
	}
}

func filterFilesToUpload(dir string, files []os.DirEntry) []string {
	var r []string
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		if !xlog.IsXLogFileName(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
		r = append(r, path)
	}
	return r
}

func uploadFiles(ctx context.Context, cfg *config.Config, stor storage.Storage, files []string) error {
	workerCount := cfg.Storage.Upload.MaxConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}

	slog.Debug("starting concurrent file uploads",
		slog.String("component", "uploader-loop"),
		slog.Int("workers", workerCount),
		slog.Int("files", len(files)),
	)

	filesChan := make(chan string, len(files))
	errorChan := make(chan error, len(files))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range filesChan {
				dumpErr := uploadOneFile(ctx, stor, filePath)
				if dumpErr != nil {
					errorChan <- dumpErr
				}
			}
		}()
	}

	// Send found files to worker chan
	for _, path := range files {
		filesChan <- path
	}
	close(filesChan) // Close the task channel once all tasks are submitted

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(errorChan)
	}()

	var lastErr error
	for e := range errorChan {
		slog.Error("file upload error",
			slog.String("component", "uploader-loop"),
			slog.Any("err", e),
		)
		lastErr = e
	}
	return lastErr
}

func uploadOneFile(ctx context.Context, stor storage.Storage, path string) error {
	slog.Info("starting upload file",
		slog.String("path", path),
		slog.String("component", "uploader-loop"),
	)

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

	slog.Info("uploaded and deleted",
		slog.String("component", "uploader-loop"),
		slog.String("path", path),
	)
	return nil
}
