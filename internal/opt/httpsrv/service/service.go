package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/model"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

var ErrAnotherOperationInProgress = errors.New("another control method is currently running")

type ControlService interface {
	Status() *xlog.StreamStatus
	RetainWALs() error
	WALArchiveSize() (*model.WALArchiveSize, error)
}
type lockInfo struct {
	task     string
	acquired time.Time
}

type constrolSvc struct {
	PGRW      *xlog.PgReceiveWal // direct access to running state
	opRunning chan lockInfo      // acts like a lock
}

var _ ControlService = &constrolSvc{}

func NewControlService(PGRW *xlog.PgReceiveWal) ControlService {
	return &constrolSvc{
		PGRW:      PGRW,
		opRunning: make(chan lockInfo, 1),
	}
}

// lock attempts to acquire the operation lock
func (s *constrolSvc) lock(task string) (ok bool, current *lockInfo) {
	select {
	case s.opRunning <- lockInfo{task: task, acquired: time.Now()}:
		return true, nil
	default:
		var info lockInfo
		select {
		case info = <-s.opRunning:
			// Put it back
			s.opRunning <- info
		default:
		}
		return false, &info
	}
}

func (s *constrolSvc) unlock() {
	select {
	case <-s.opRunning:
	default:
	}
}

func (s *constrolSvc) Status() *xlog.StreamStatus {
	// read-only; doesn’t need to block
	return s.PGRW.Status()
}

func (s *constrolSvc) RetainWALs() error {
	ok, current := s.lock("RetainWALs")
	if !ok {
		return fmt.Errorf(
			"cannot run RetainWALs: %s is already in progress since %s",
			current.task,
			current.acquired.Format(time.RFC3339),
		)
	}
	defer s.unlock()

	// Long-running cleanup here...
	time.Sleep(5 * time.Second)
	return nil
}

func (s *constrolSvc) WALArchiveSize() (*model.WALArchiveSize, error) {
	// read-only; doesn’t need to block

	size, err := optutils.DirSize(s.PGRW.BaseDir, &optutils.DirSizeOpts{
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
