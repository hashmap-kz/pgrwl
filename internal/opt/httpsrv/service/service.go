package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/model"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ControlService interface {
	Status() *model.PgRwlStatus
	RetainWALs() error
	WALArchiveSize() (*model.WALArchiveSize, error)
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}
type lockInfo struct {
	task     string
	acquired time.Time
}

type controlSvc struct {
	pgrw        xlog.PgReceiveWal // direct access to running state
	baseDir     string
	runningMode string
	storage     *storage.TransformingStorage

	mu   sync.Mutex // protects access to `lock`
	held bool       // is the lock currently held?
	info lockInfo   // metadata about the lock
}

var _ ControlService = &controlSvc{}

type ControlServiceOpts struct {
	PGRW        xlog.PgReceiveWal
	BaseDir     string
	RunningMode string
	Storage     *storage.TransformingStorage
}

func NewControlService(opts *ControlServiceOpts) ControlService {
	return &controlSvc{
		pgrw:        opts.PGRW,
		baseDir:     opts.BaseDir,
		runningMode: opts.RunningMode,
		storage:     opts.Storage,
	}
}

// tryLock attempts to acquire the operation lock
func (s *controlSvc) tryLock(task string) (bool, *lockInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.held {
		// Copy so caller can safely read
		info := s.info
		return false, &info
	}

	s.held = true
	s.info = lockInfo{
		task:     task,
		acquired: time.Now(),
	}
	return true, nil
}

func (s *controlSvc) unlock() {
	s.mu.Lock()
	s.held = false
	s.info = lockInfo{} // clear metadata
	s.mu.Unlock()
}

func (s *controlSvc) Status() *model.PgRwlStatus {
	// read-only; doesn’t need to block

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

func (s *controlSvc) RetainWALs() error {
	ok, current := s.tryLock("RetainWALs")
	if !ok {
		return fmt.Errorf("cannot run RetainWALs: %s is already running since %s",
			current.task, current.acquired.Format(time.RFC3339))
	}
	defer s.unlock()

	// Long-running cleanup here...
	time.Sleep(5 * time.Second)
	return nil
}

func (s *controlSvc) WALArchiveSize() (*model.WALArchiveSize, error) {
	// read-only; doesn’t need to block

	size, err := optutils.DirSize(s.baseDir, &optutils.DirSizeOpts{
		IgnoreErrPermission: true,
		IgnoreErrNotExist:   true,
	})
	if err != nil {
		return nil, err
	}
	return &model.WALArchiveSize{
		Bytes: size,
		IEC:   optutils.ByteCountIEC(size),
	}, nil
}

func (s *controlSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	// 1) Fast-path: check that file exists locally
	// 2) Check *.partial file locally
	// 3) Fetch from storage (if it's not nil)

	// TODO: local storage
	// TODO: send checksum in headers

	if s.storage == nil {
		filePath := filepath.Join(s.baseDir, filename)
		partialFilePath := filePath + xlog.PartialSuffix

		slog.Debug("wal-restore, fetching local file", slog.String("path", filePath))
		if optutils.FileExists(filePath) {
			slog.Debug("wal-restore, found local file", slog.String("path", filePath))
			return os.Open(filePath)
		}
		if optutils.FileExists(partialFilePath) {
			slog.Debug("wal-restore, found local partial file", slog.String("path", partialFilePath))
			return os.Open(partialFilePath)
		}
		return nil, fmt.Errorf("cannot find local file: %s", filePath)
	}

	slog.Debug("wal-restore, fetching remote file", slog.String("filename", filename))
	tarFile, err := s.storage.Get(ctx, filename+".tar")
	if err != nil {
		return nil, err
	}
	return optutils.GetFileFromTar(tarFile, filename)
}
