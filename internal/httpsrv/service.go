package httpsrv

import (
	"time"

	"github.com/hashmap-kz/pgrwl/internal/xlog"
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
