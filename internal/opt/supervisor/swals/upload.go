package swals

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

func (u *ArchiveSupervisor) performUploads(ctx context.Context) error {
	files, err := os.ReadDir(u.opts.ReceiveDirectory)
	if err != nil {
		u.log().Error("error reading dir", slog.Any("err", err))
		return err
	}
	filesToUpload := u.filterFilesToUpload(files)
	if len(filesToUpload) == 0 {
		return nil
	}
	return u.uploadFiles(ctx, filesToUpload)
}

func (u *ArchiveSupervisor) filterFilesToUpload(files []os.DirEntry) []uploadBundle {
	r := make([]uploadBundle, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Base(name) == ".manifest.json" {
			continue
		}
		currentOpenWALFileName := u.opts.PGRW.CurrentOpenWALFileName()
		if filepath.Base(name) == filepath.Base(currentOpenWALFileName) {
			u.log().Debug("skipped currently opened file", slog.String("path", filepath.ToSlash(name)))
			continue
		}
		walFilePath := filepath.ToSlash(filepath.Join(u.opts.ReceiveDirectory, name))
		if !optutils.FileExists(walFilePath) {
			continue
		}
		r = append(r, uploadBundle{
			walFilePath: walFilePath,
		})
	}
	return r
}

func (u *ArchiveSupervisor) uploadFiles(ctx context.Context, files []uploadBundle) error {
	workerCount := u.cfg.Receiver.Uploader.MaxConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}

	u.log().Debug("starting concurrent file uploads",
		slog.Int("workers", workerCount),
		slog.Int("files", len(files)),
	)

	filesChan := make(chan uploadBundle, len(files))
	errorChan := make(chan error, len(files))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range filesChan {
				// Check for cancellation
				if ctx.Err() != nil {
					return
				}
				err := u.uploadOneFile(ctx, filePath)
				if err != nil {
					select {
					case errorChan <- err:
					default:
					}
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
		u.log().Error("file upload error",
			slog.Any("err", e),
		)
		lastErr = e
	}
	return lastErr
}

func (u *ArchiveSupervisor) uploadOneFile(ctx context.Context, bundle uploadBundle) error {
	u.log().Info("starting upload file",
		slog.String("path", bundle.walFilePath),
	)

	file, err := os.Open(bundle.walFilePath)
	if err != nil {
		return err
	}

	resultFileName := filepath.Base(bundle.walFilePath)

	err = u.stor.Put(ctx, resultFileName, file)
	if err != nil {
		// upload error: close the file, return err, DO NOT REMOVE SOURCE WHEN UPLOAD IS FAILED
		_ = file.Close()
		return err
	}

	// upload success: closing file
	if err := file.Close(); err != nil {
		return err
	}

	// remove files when upload is success
	if err := os.Remove(bundle.walFilePath); err != nil {
		return err
	}

	u.log().Info("uploaded and deleted",
		slog.String("wal-path", bundle.walFilePath),
		slog.String("result-path", resultFileName),
	)

	metrics.M.IncWALFilesUploaded()
	return nil
}
