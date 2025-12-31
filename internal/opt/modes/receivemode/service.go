package receivemode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics/receivemetrics"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/fsx"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

type Service interface {
	Status() *PgRwlStatus
	DeleteWALsBefore(ctx context.Context, walFileName string) error
	BriefConfig(ctx context.Context) *BriefConfig
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}

type receiveModeSvc struct {
	l    *slog.Logger
	opts *ReceiveDaemonRunOpts
}

var _ Service = &receiveModeSvc{}

func NewReceiveModeService(opts *ReceiveDaemonRunOpts) Service {
	return &receiveModeSvc{
		l:    slog.With("component", "receive-service"),
		opts: opts,
	}
}

func (s *receiveModeSvc) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}
	return slog.With("component", "receive-service")
}

func (s *receiveModeSvc) Status() *PgRwlStatus {
	s.log().Debug("querying status")

	var streamStatusResp *StreamStatus
	if s.opts.PGRW != nil {
		streamStatus := s.opts.PGRW.Status()
		streamStatusResp = &StreamStatus{
			Slot:         streamStatus.Slot,
			Timeline:     streamStatus.Timeline,
			LastFlushLSN: streamStatus.LastFlushLSN,
			Uptime:       streamStatus.Uptime,
			Running:      streamStatus.Running,
		}
	}
	return &PgRwlStatus{
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

func (s *receiveModeSvc) DeleteWALsBefore(_ context.Context, walFileName string) error {
	if s.opts.JobQueue != nil {
		err := s.opts.JobQueue.Submit("delete-wal-before-"+walFileName, func(_ context.Context) {
			s.log().Info("deleting WAL files")
			walFilesInStorage, err := s.opts.Storage.List(context.Background(), "")
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

			if s.opts.Verbose {
				s.log().LogAttrs(context.Background(), logger.LevelTrace, "begin to delete wal files")
				for _, w := range walFilesToDelete {
					s.log().LogAttrs(context.Background(), logger.LevelTrace, "wal file to delete",
						slog.String("name", w),
					)
				}
			}

			err = s.opts.Storage.DeleteAllBulk(context.Background(), walFilesToDelete)
			if err != nil {
				s.log().Error("cannot delete WAL files",
					slog.String("before", walFileName),
					slog.Any("err", err),
				)
				return
			}

			// update metrics
			receivemetrics.M.AddWALFilesDeleted(float64(len(walFilesToDelete)))
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *receiveModeSvc) BriefConfig(_ context.Context) *BriefConfig {
	cfg := config.Cfg()
	return &BriefConfig{RetentionEnable: cfg.Receiver.Retention.Enable}
}

func (s *receiveModeSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	// 1) Fast-path: check that file exists locally
	// 2) Check *.partial file locally
	// 3) Fetch from storage (if it's not nil)

	// TODO: send checksum in headers

	s.log().Debug("fetching WAL file", slog.String("filename", filename))

	// 1) trying to find local completed segment
	// 2) trying to find partial segment
	filePath := filepath.Join(s.opts.BaseDir, filename)
	partialFilePath := filePath + xlog.PartialSuffix

	s.log().Debug("wal-restore, fetching local file", slog.String("path", filePath))
	if fsx.FileExists(filePath) {
		s.log().Debug("wal-restore, found local file", slog.String("path", filePath))
		return os.Open(filePath)
	}
	if fsx.FileExists(partialFilePath) {
		s.log().Debug("wal-restore, found local partial file", slog.String("path", partialFilePath))
		return os.Open(partialFilePath)
	}

	// 3) trying remote
	if s.opts.Storage != nil {
		s.log().Debug("wal-restore, fetching remote file", slog.String("filename", filename))
		return s.opts.Storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("cannot fetch file: %s", filename)
}
