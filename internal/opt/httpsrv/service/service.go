package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/model"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ControlService interface {
	Status() *xlog.StreamStatus
	RetainWALs() error
	WALArchiveSize() (*model.WALArchiveSize, error)
}
type lockInfo struct {
	task     string
	acquired time.Time
}

type controlSvc struct {
	pgrw *xlog.PgReceiveWal // direct access to running state

	mu   sync.Mutex // protects access to `lock`
	held bool       // is the lock currently held?
	info lockInfo   // metadata about the lock
}

var _ ControlService = &controlSvc{}

func NewControlService(pgrw *xlog.PgReceiveWal) ControlService {
	return &controlSvc{
		pgrw: pgrw,
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

func (s *controlSvc) Status() *xlog.StreamStatus {
	// read-only; doesn’t need to block
	return s.pgrw.Status()
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

	size, err := optutils.DirSize(s.pgrw.BaseDir, &optutils.DirSizeOpts{
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
