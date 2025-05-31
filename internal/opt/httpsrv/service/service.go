package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"

	"github.com/hashmap-kz/pgrwl/internal/jobq"
	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/model"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ControlService interface {
	Status() *model.PgRwlStatus
	DeleteWALsBefore(ctx context.Context, walFileName string) error
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}

type controlSvc struct {
	l           *slog.Logger
	pgrw        xlog.PgReceiveWal // direct access to running state
	baseDir     string
	runningMode string
	storage     *storage.TransformingStorage
	jobQueue    *jobq.JobQueue // optional, nil in 'serve' mode
	verbose     bool
}

var _ ControlService = &controlSvc{}

type ControlServiceOpts struct {
	PGRW        xlog.PgReceiveWal
	BaseDir     string
	RunningMode string
	Storage     *storage.TransformingStorage
	JobQueue    *jobq.JobQueue // optional, nil in 'serve' mode
	Verbose     bool
}

func NewControlService(opts *ControlServiceOpts) ControlService {
	return &controlSvc{
		l:           slog.With("component", "control-service"),
		pgrw:        opts.PGRW,
		baseDir:     opts.BaseDir,
		runningMode: opts.RunningMode,
		storage:     opts.Storage,
		jobQueue:    opts.JobQueue,
		verbose:     opts.Verbose,
	}
}

func (s *controlSvc) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}
	return slog.With("component", "control-service")
}

func (s *controlSvc) Status() *model.PgRwlStatus {
	s.log().Debug("querying status")

	var streamStatusResp *model.StreamStatus
	if s.pgrw != nil {
		streamStatus := s.pgrw.Status()
		streamStatusResp = &model.StreamStatus{
			Slot:         streamStatus.Slot,
			Timeline:     streamStatus.Timeline,
			LastFlushLSN: streamStatus.LastFlushLSN,
			Uptime:       streamStatus.Uptime,
			Running:      streamStatus.Running,
		}
	}
	return &model.PgRwlStatus{
		RunningMode:  s.runningMode,
		StreamStatus: streamStatusResp,
	}
}

// filterWalBefore returns a list of WAL file paths where the file name is lexically less than the cutoff WAL name.
func filterWalBefore(walFiles []string, cutoff string) []string {
	slices.Sort(walFiles)

	toDelete := []string{}
	for _, walPath := range walFiles {
		filename := filepath.Base(walPath)
		if len(filename) < xlog.XLogFileNameLen {
			continue
		}
		if !xlog.IsXLogFileName(filename[:24]) {
			continue
		}
		if filename < cutoff {
			toDelete = append(toDelete, walPath)
		}
	}
	return toDelete
}

func (s *controlSvc) DeleteWALsBefore(_ context.Context, walFileName string) error {
	if s.jobQueue != nil {
		err := s.jobQueue.Submit("delete-wal-before-"+walFileName, func(_ context.Context) {
			s.log().Info("deleting WAL files")
			walFilesInStorage, err := s.storage.List(context.Background(), "")
			if err != nil {
				s.log().Error("cannot delete WAL files",
					slog.String("before", walFileName),
					slog.Any("err", err),
				)
				return
			}
			walFilesToDelete := filterWalBefore(walFilesInStorage, walFileName)
			if len(walFilesToDelete) == 0 {
				return
			}

			if s.verbose {
				s.log().LogAttrs(context.Background(), logger.LevelTrace, "begin to delete wal files")
				for _, w := range walFilesToDelete {
					s.log().LogAttrs(context.Background(), logger.LevelTrace, "wal file to delete",
						slog.String("name", w),
					)
				}
			}

			err = s.storage.DeleteAllBulk(context.Background(), walFilesToDelete)
			if err != nil {
				s.log().Error("cannot delete WAL files",
					slog.String("before", walFileName),
					slog.Any("err", err),
				)
				return
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *controlSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	// 1) Fast-path: check that file exists locally
	// 2) Check *.partial file locally
	// 3) Fetch from storage (if it's not nil)

	// TODO: send checksum in headers

	s.log().Debug("fetching WAL file", slog.String("filename", filename))

	// 1) trying to find local completed segment
	// 2) trying to find partial segment
	filePath := filepath.Join(s.baseDir, filename)
	partialFilePath := filePath + xlog.PartialSuffix

	s.log().Debug("wal-restore, fetching local file", slog.String("path", filePath))
	if optutils.FileExists(filePath) {
		s.log().Debug("wal-restore, found local file", slog.String("path", filePath))
		return os.Open(filePath)
	}
	if optutils.FileExists(partialFilePath) {
		s.log().Debug("wal-restore, found local partial file", slog.String("path", partialFilePath))
		return os.Open(partialFilePath)
	}

	// 3) trying remote
	if s.storage != nil {
		s.log().Debug("wal-restore, fetching remote file", slog.String("filename", filename))
		return s.storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("cannot fetch file: %s", filename)
}
