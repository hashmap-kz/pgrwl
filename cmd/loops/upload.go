package loops

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"

	"github.com/hashmap-kz/pgrwl/cmd/cmdutils"
	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

type UploaderLoopOpts struct {
	ReceiveDirectory string
	PGRW             xlog.PgReceiveWal
}

type uploadBundle struct {
	walFilePath string
}

type Uploader struct {
	l    *slog.Logger
	mu   sync.Mutex
	cfg  *config.Config
	stor storage.Storage
	opts *UploaderLoopOpts
}

func NewUploader(cfg *config.Config, stor storage.Storage, opts *UploaderLoopOpts) *Uploader {
	return &Uploader{
		l:    slog.With(slog.String("component", "uploader")),
		cfg:  cfg,
		stor: stor,
		opts: opts,
	}
}

func (u *Uploader) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}
	return slog.With(slog.String("component", "uploader"))
}

func (u *Uploader) RunUploader(ctx context.Context) {
	syncInterval := cmdutils.ParseDurationOrDefault(u.cfg.Uploader.SyncInterval, 30*time.Second)

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting...")
			return
		case <-ticker.C:
			u.log().Debug("upload worker is running")
			err := u.performUploads(ctx)
			u.log().Debug("upload worker is done")
			if err != nil {
				u.log().Error("error upload files", slog.Any("err", err))
			}
		}
	}
}

func (u *Uploader) RunWithRetention(ctx context.Context, daysKeepRetention time.Duration) {
	syncInterval := cmdutils.ParseDurationOrDefault(u.cfg.Uploader.SyncInterval, 30*time.Second)
	retentionInterval := cmdutils.ParseDurationOrDefault(u.cfg.Retention.SyncInterval, 24*time.Hour)

	uploadTicker := time.NewTicker(syncInterval)
	retentionTicker := time.NewTicker(retentionInterval)
	defer uploadTicker.Stop()
	defer retentionTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting...")
			return
		case <-uploadTicker.C:
			u.log().Debug("upload worker is running")
			u.mu.Lock()
			err := u.performUploads(ctx)
			u.mu.Unlock()
			u.log().Debug("upload worker is done")
			if err != nil {
				u.log().Error("error upload files", slog.Any("err", err))
			}
		case <-retentionTicker.C:
			u.log().Debug("retention worker is running")
			u.mu.Lock()
			err := u.performRetention(ctx, daysKeepRetention)
			u.mu.Unlock()
			u.log().Debug("retention worker is done")
			if err != nil {
				u.log().Error("error retain files", slog.Any("err", err))
			}
		}
	}
}

func (u *Uploader) performUploads(ctx context.Context) error {
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

func (u *Uploader) filterOlderThan(files []storage.FileInfo, maxAge time.Duration) []storage.FileInfo {
	var result []storage.FileInfo
	cutoff := time.Now().Add(-maxAge)
	for _, f := range files {
		if f.ModTime.Before(cutoff) {
			result = append(result, f)
		}
	}
	return result
}

func (u *Uploader) performRetention(ctx context.Context, daysKeepRetention time.Duration) error {
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
	return nil
}

func (u *Uploader) filterFilesToUpload(files []os.DirEntry) []uploadBundle {
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

func (u *Uploader) uploadFiles(ctx context.Context, files []uploadBundle) error {
	workerCount := u.cfg.Uploader.MaxConcurrency
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

func (u *Uploader) uploadOneFile(ctx context.Context, bundle uploadBundle) error {
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
	return nil
}
