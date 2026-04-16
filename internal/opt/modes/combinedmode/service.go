package combinedmode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/shared/x/fsx"
)

// Service exposes all operations needed by the combined-mode HTTP layer.
type Service interface {
	// ReceiverStart - Receiver control
	ReceiverStart() error
	ReceiverStop()
	ReceiverState() xlog.ReceiverState
	ReceiverStatus() *xlog.StreamStatus

	// GetWalFile - WAL serving (used by restore_command)
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}

type combinedSvc struct {
	l        *slog.Logger
	receiver *xlog.RestartablePgReceiver
	baseDir  string
	storage  *st.VariadicStorage
}

type ServiceOpts struct {
	Receiver *xlog.RestartablePgReceiver
	BaseDir  string
	Storage  *st.VariadicStorage
}

func NewService(opts *ServiceOpts) Service {
	return &combinedSvc{
		l:        slog.With("component", "combined-service"),
		receiver: opts.Receiver,
		baseDir:  opts.BaseDir,
		storage:  opts.Storage,
	}
}

func (s *combinedSvc) ReceiverStart() error {
	return s.receiver.Start()
}

func (s *combinedSvc) ReceiverStop() {
	s.receiver.Stop()
}

func (s *combinedSvc) ReceiverState() xlog.ReceiverState {
	return s.receiver.State()
}

func (s *combinedSvc) ReceiverStatus() *xlog.StreamStatus {
	return s.receiver.Status()
}

// GetWalFile mirrors servemode.GetWalFile:
//  1. local completed segment
//  2. local *.partial segment
//  3. remote storage (if configured)
func (s *combinedSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	filePath := filepath.Join(s.baseDir, filename)
	partialPath := filePath + xlog.PartialSuffix

	if fsx.FileExists(filePath) {
		s.l.Debug("wal-restore: found local file", slog.String("path", filePath))
		//nolint:gosec
		return os.Open(filePath)
	}
	if fsx.FileExists(partialPath) {
		s.l.Debug("wal-restore: found local partial file", slog.String("path", partialPath))
		//nolint:gosec
		return os.Open(partialPath)
	}
	if s.storage != nil {
		s.l.Debug("wal-restore: fetching remote file", slog.String("filename", filename))
		return s.storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("cannot fetch WAL file: %s", filename)
}
