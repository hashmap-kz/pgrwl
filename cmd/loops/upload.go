package loops

import (
	"archive/tar"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	StatusDirectory  string
}

type uploadBundle struct {
	doneFilePath string
	walFilePath  string
}

func RunUploaderLoop(ctx context.Context, cfg *config.Config, stor storage.Storage, opts *UploaderLoopOpts) {
	syncInterval := cmdutils.ParseDurationOrDefault(cfg.Storage.Upload.SyncInterval, 30*time.Second)

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("(uploader-loop) context is done, exiting...")
			return
		case <-ticker.C:
			files, err := os.ReadDir(opts.StatusDirectory)
			if err != nil {
				slog.Error("error reading dir",
					slog.String("component", "uploader-loop"),
					slog.Any("err", err),
				)
				continue
			}

			filesToUpload := filterFilesToUpload(opts, files)
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

func filterFilesToUpload(opts *UploaderLoopOpts, files []os.DirEntry) []uploadBundle {
	r := make([]uploadBundle, 0, len(files))
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		doneFilePath := filepath.ToSlash(filepath.Join(opts.StatusDirectory, name))
		if !strings.HasSuffix(name, xlog.DoneMarkerFileExt) {
			removeStrayDoneMarkerFile(doneFilePath)
			continue
		}
		name = strings.TrimSuffix(name, xlog.DoneMarkerFileExt)
		if !xlog.IsXLogFileName(name) {
			removeStrayDoneMarkerFile(doneFilePath)
			continue
		}
		walFilePath := filepath.ToSlash(filepath.Join(opts.ReceiveDirectory, name))
		if !optutils.FileExists(walFilePath) {
			// misconfigured, etc, we may safely delete *.done file here
			removeStrayDoneMarkerFile(doneFilePath)
			continue
		}
		r = append(r, uploadBundle{
			doneFilePath: doneFilePath,
			walFilePath:  walFilePath,
		})
	}
	return r
}

func removeStrayDoneMarkerFile(doneFilePath string) {
	err := os.Remove(doneFilePath)
	if err == nil {
		slog.Warn("stray *.done marker file is removed",
			slog.String("path", doneFilePath),
		)
	} else {
		slog.Error("cannot remove stray *.done marker file",
			slog.String("path", doneFilePath),
		)
	}
}

func uploadFiles(ctx context.Context, cfg *config.Config, stor storage.Storage, files []uploadBundle) error {
	workerCount := cfg.Storage.Upload.MaxConcurrency
	if workerCount <= 0 {
		workerCount = 1
	}

	slog.Debug("starting concurrent file uploads",
		slog.String("component", "uploader-loop"),
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
				err := uploadOneFile(ctx, stor, filePath)
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
		slog.Error("file upload error",
			slog.String("component", "uploader-loop"),
			slog.Any("err", e),
		)
		lastErr = e
	}
	return lastErr
}

func uploadOneFile(ctx context.Context, stor storage.Storage, bundle uploadBundle) error {
	slog.Info("starting upload file",
		slog.String("path", bundle.walFilePath),
		slog.String("component", "uploader-loop"),
	)

	tarReader := createTarReader([]string{
		bundle.doneFilePath,
		bundle.walFilePath,
	})

	resultFileName := filepath.Base(bundle.walFilePath) + ".tar"

	err := stor.Put(ctx, resultFileName, tarReader)
	if err != nil {
		// upload error: close the file, return err, DO NOT REMOVE SOURCE WHEN UPLOAD IS FAILED
		_ = tarReader.Close()
		return err
	}

	// upload success: closing file
	if err := tarReader.Close(); err != nil {
		return err
	}

	// remove files when upload is success
	if err := os.Remove(bundle.doneFilePath); err != nil {
		return err
	}
	if err := os.Remove(bundle.walFilePath); err != nil {
		return err
	}

	slog.Info("uploaded and deleted",
		slog.String("component", "uploader-loop"),
		slog.String("done-marker-path", bundle.doneFilePath),
		slog.String("wal-path", bundle.walFilePath),
		slog.String("result-path", resultFileName),
	)
	return nil
}

func createTarReader(files []string) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		tw := tar.NewWriter(pw)
		defer tw.Close()

		for _, file := range files {
			err := func() error {
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				defer f.Close()

				stat, err := f.Stat()
				if err != nil {
					return err
				}

				header, err := tar.FileInfoHeader(stat, "")
				if err != nil {
					return err
				}
				header.Name = filepath.Base(file)
				if err := tw.WriteHeader(header); err != nil {
					return err
				}
				_, err = io.Copy(tw, f)
				return err
			}()
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
	}()

	return pr
}
