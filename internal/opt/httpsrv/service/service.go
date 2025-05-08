package service

import (
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv/model"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"
)

type ControlService struct {
	PGRW *xlog.PgReceiveWal // direct access to running state
}

func (s *ControlService) Status() *xlog.StreamStatus {
	return s.PGRW.Status()
}

func (s *ControlService) RetainWALs() error {
	// Long-running cleanup here...
	time.Sleep(5 * time.Second)
	return nil
}

func (s *ControlService) WALArchiveSize() (*model.WALArchiveSize, error) {
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
