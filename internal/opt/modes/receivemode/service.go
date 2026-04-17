package receivemode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"github.com/pgrwl/pgrwl/internal/core/logger"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/fsx"

	"github.com/pgrwl/pgrwl/config"
)

// Service is the full interface exposed by receive mode.
type Service interface {
	// Receiver lifecycle
	ReceiverStart() error
	ReceiverStop()
	ReceiverStatus() *ReceiverStatus

	// WAL file serving - used by restore_command via GET /wal/{filename}
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)

	// WAL deletion - scheduled via DELETE /wal-before/{filename}
	DeleteWALsBefore(ctx context.Context, walFileName string) error

	// Config introspection - used by backup supervisor via GET /config
	BriefConfig(ctx context.Context) *BriefConfig
}

type receiveModeSvc struct {
	l        *slog.Logger
	receiver *xlog.RestartablePgReceiver
	baseDir  string
	storage  *st.VariadicStorage
	jobQueue *jobq.JobQueue
	verbose  bool
}

var _ Service = &receiveModeSvc{}

// ServiceOpts groups the dependencies injected at construction time.
type ServiceOpts struct {
	Receiver *xlog.RestartablePgReceiver
	BaseDir  string
	Storage  *st.VariadicStorage
	JobQueue *jobq.JobQueue
	Verbose  bool
}

func NewService(opts *ServiceOpts) Service {
	return &receiveModeSvc{
		l:        slog.With("component", "receive-service"),
		receiver: opts.Receiver,
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

// Receiver lifecycle

func (s *receiveModeSvc) ReceiverStart() error {
	return s.receiver.Start()
}

func (s *receiveModeSvc) ReceiverStop() {
	s.receiver.Stop()
}

func (s *receiveModeSvc) ReceiverStatus() *ReceiverStatus {
	s.log().Debug("querying receiver status")

	state := s.receiver.State()
	status := &ReceiverStatus{State: state}

	if state == xlog.ReceiverStateRunning {
		inner := s.receiver.Status()
		status.Stream = &StreamStatus{
			Slot:         inner.Slot,
			Timeline:     inner.Timeline,
			LastFlushLSN: inner.LastFlushLSN,
			Uptime:       inner.Uptime,
			Running:      inner.Running,
		}
	}

	return status
}

// WAL file serving

// GetWalFile resolves a WAL file for restore_command.
//
// Resolution order:
//  1. Local completed segment (baseDir/<filename>)
//  2. Local partial segment  (baseDir/<filename>.partial)
//  3. Remote storage, if configured
func (s *receiveModeSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	s.log().Debug("fetching WAL file", slog.String("filename", filename))

	filePath := filepath.Join(s.baseDir, filename)
	partialPath := filePath + xlog.PartialSuffix

	if fsx.FileExists(filePath) {
		s.log().Debug("wal-restore: found local file", slog.String("path", filePath))
		//nolint:gosec
		return os.Open(filePath)
	}
	if fsx.FileExists(partialPath) {
		s.log().Debug("wal-restore: found local partial file", slog.String("path", partialPath))
		//nolint:gosec
		return os.Open(partialPath)
	}
	if s.storage != nil {
		s.log().Debug("wal-restore: fetching remote file", slog.String("filename", filename))
		return s.storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("WAL file not found: %s", filename)
}

// WAL deletion

func (s *receiveModeSvc) DeleteWALsBefore(_ context.Context, walFileName string) error {
	if s.jobQueue == nil {
		return nil
	}
	return s.jobQueue.Submit("delete-wal-before-"+walFileName, func(_ context.Context) {
		s.log().Info("deleting WAL files", slog.String("before", walFileName))

		walFiles, err := s.storage.List(context.Background(), "")
		if err != nil {
			s.log().Error("cannot list WAL files for deletion",
				slog.String("before", walFileName),
				slog.Any("err", err),
			)
			return
		}

		toDelete := filterWalBefore(walFiles, walFileName)
		if len(toDelete) == 0 {
			return
		}

		if s.verbose {
			for _, w := range toDelete {
				s.log().LogAttrs(context.Background(), logger.LevelTrace, "WAL file to delete",
					slog.String("name", w),
				)
			}
		}

		if err := s.storage.DeleteAllBulk(context.Background(), toDelete); err != nil {
			s.log().Error("cannot delete WAL files",
				slog.String("before", walFileName),
				slog.Any("err", err),
			)
			return
		}

		receivemetrics.M.AddWALFilesDeleted(float64(len(toDelete)))
	})
}

// filterWalBefore returns the paths whose base filename is lexically less than
// cutoff and looks like a valid WAL segment name.
func filterWalBefore(walFiles []string, cutoff string) []string {
	slices.Sort(walFiles)
	var out []string
	for _, p := range walFiles {
		base := filepath.Base(p)
		if len(base) < xlog.XLogFileNameLen {
			continue
		}
		if !xlog.IsXLogFileName(base[:24]) {
			continue
		}
		if base < cutoff {
			out = append(out, p)
		}
	}
	return out
}

// Config introspection

func (s *receiveModeSvc) BriefConfig(_ context.Context) *BriefConfig {
	cfg := config.Cfg()
	return &BriefConfig{RetentionEnable: cfg.Receiver.Retention.Enable}
}
