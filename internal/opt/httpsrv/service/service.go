package service

import (
	"errors"
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

type constrolSvc struct {
	PGRW      *xlog.PgReceiveWal // direct access to running state
	opRunning chan struct{}      // acts like a lock
}

var _ ControlService = &constrolSvc{}

func NewControlService(PGRW *xlog.PgReceiveWal) ControlService {
	return &constrolSvc{
		PGRW:      PGRW,
		opRunning: make(chan struct{}, 1),
	}
}

// lock attempts to acquire the operation lock
func (s *constrolSvc) lock() bool {
	select {
	case s.opRunning <- struct{}{}:
		return true
	default:
		return false
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
	if !s.lock() {
		return ErrAnotherOperationInProgress
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
