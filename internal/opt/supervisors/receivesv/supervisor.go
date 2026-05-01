package receivesv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type ArchiveSupervisorOpts struct {
	ReceiveDirectory string
	PGRW             xlog.PgReceiveWal
}

type uploadBundle struct {
	walFilePath string
}

type ArchiveSupervisor struct {
	l    *slog.Logger
	cfg  *config.Config
	stor st.Storage
	opts *ArchiveSupervisorOpts

	// storageName is kept as a fast-access copy to avoid repeatedly walking cfg.
	storageName string
}

func NewArchiveSupervisor(cfg *config.Config, stor st.Storage, opts *ArchiveSupervisorOpts) *ArchiveSupervisor {
	return &ArchiveSupervisor{
		l:           slog.With(slog.String("component", "archive-supervisor")),
		cfg:         cfg,
		stor:        stor,
		opts:        opts,
		storageName: cfg.Storage.Name,
	}
}

func (u *ArchiveSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}

	return slog.With(slog.String("component", "archive-supervisor"))
}

// TODO: all-in-one - not needed after merging receive+backup (no wal-retention)

// Run schedules archive maintenance jobs until ctx is cancelled.
//
// The queue is intentionally single-worker. upload and retain jobs must not run
// at the same time because both can inspect or mutate WAL archive state.
//
// Submit is used so slow periodic jobs do not pile up behind themselves.
// For example, if an upload takes longer than one upload interval, the next
// upload tick is dropped instead of queueing stale duplicate upload work.
func (u *ArchiveSupervisor) Run(ctx context.Context, queue *jobq.JobQueue) error {
	if queue == nil {
		return fmt.Errorf("job queue is nil")
	}

	uploadInterval := u.cfg.Receiver.Uploader.SyncIntervalParsed
	if uploadInterval <= 0 {
		return fmt.Errorf("invalid upload sync interval: %s", uploadInterval)
	}

	uploadTicker := time.NewTicker(uploadInterval)
	defer uploadTicker.Stop()

	u.log().Info("archive supervisor started",
		slog.Duration("upload_interval", uploadInterval),
	)

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting archive supervisor")
			return ctx.Err()

		case <-uploadTicker.C:
			if err := queue.Submit("upload", func(ctx context.Context) {
				u.log().Debug("upload worker is running")
				defer u.log().Debug("upload worker is done")

				if err := u.performUploads(ctx); err != nil {
					if errors.Is(err, context.Canceled) {
						u.log().Info("upload worker stopped", slog.Any("reason", err))
						return
					}

					u.log().Error("error uploading files", slog.Any("err", err))
				}
			}); err != nil {
				u.log().Warn("upload job was not submitted", slog.Any("err", err))
				continue
			}
		}
	}
}
