package receivemode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"

	"github.com/pgrwl/pgrwl/config"

	"github.com/pgrwl/pgrwl/internal/opt/jobq"

	"github.com/pgrwl/pgrwl/internal/core/logger"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/fsx"
)

type Service interface {
	Status() *PgRwlStatus
	DeleteWALsBefore(ctx context.Context, walFileName string) error
	BriefConfig(ctx context.Context) *BriefConfig

	// Receiver lifecycle control (used by POST /receiver and GET /receiver).
	ReceiverStart() error
	ReceiverStop()
	ReceiverState() xlog.ReceiverState
	ReceiverStatus() *xlog.StreamStatus

	// WAL file serving (used by GET /wal/{filename} for restore_command).
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}

type receiveModeSvc struct {
	l        *slog.Logger
	pgrw     *xlog.RestartablePgReceiver // restartable receiver; Start/Stop via API
	baseDir  string
	storage  *st.VariadicStorage
	jobQueue *jobq.JobQueue // optional, nil in 'serve' mode
	verbose  bool
}

var _ Service = &receiveModeSvc{}

type ReceiveServiceOpts struct {
	PGRW     *xlog.RestartablePgReceiver
	BaseDir  string
	Storage  *st.VariadicStorage
	JobQueue *jobq.JobQueue // optional, nil in 'serve' mode
	Verbose  bool
}

func NewReceiveModeService(opts *ReceiveServiceOpts) Service {
	return &receiveModeSvc{
		l:        slog.With("component", "receive-service"),
		pgrw:     opts.PGRW,
		baseDir:  opts.BaseDir,
		storage:  opts.Storage,
		jobQueue: opts.JobQueue,
		verbose:  opts.Verbose,
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
	if s.pgrw != nil {
		streamStatus := s.pgrw.Status()
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
	if s.jobQueue != nil && s.storage != nil {
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

func (s *receiveModeSvc) ReceiverStart() error {
	return s.pgrw.Start()
}

func (s *receiveModeSvc) ReceiverStop() {
	s.pgrw.Stop()
}

func (s *receiveModeSvc) ReceiverState() xlog.ReceiverState {
	return s.pgrw.State()
}

func (s *receiveModeSvc) ReceiverStatus() *xlog.StreamStatus {
	return s.pgrw.Status()
}

func (s *receiveModeSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
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
	if fsx.FileExists(filePath) {
		s.log().Debug("wal-restore, found local file", slog.String("path", filePath))
		//nolint:gosec
		return os.Open(filePath)
	}
	if fsx.FileExists(partialFilePath) {
		s.log().Debug("wal-restore, found local partial file", slog.String("path", partialFilePath))
		//nolint:gosec
		return os.Open(partialFilePath)
	}

	// 3) trying remote
	if s.storage != nil {
		s.log().Debug("wal-restore, fetching remote file", slog.String("filename", filename))
		return s.storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("cannot fetch file: %s", filename)
}
