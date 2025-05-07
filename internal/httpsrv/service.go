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

type WALArchiveSize struct {
	Bytes int64  `json:"bytes,omitempty"`
	IEC   string `json:"iec,omitempty"`
}

func (s *ControlService) WALArchiveSize() (*WALArchiveSize, error) {
	size, err := DirSize(s.PGRW.BaseDir, &DirSizeOpts{
		IgnoreErrPermission: true,
		IgnoreErrNotExist:   true,
	})
	if err != nil {
		return nil, err
	}
	return &WALArchiveSize{
		Bytes: size,
		IEC:   ByteCountIEC(size),
	}, nil
}
