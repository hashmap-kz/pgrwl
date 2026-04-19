package receivesuperv

import (
	"context"
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

// Run schedules archive maintenance jobs until ctx is cancelled.
//
// The queue is intentionally single-worker. upload and retain jobs must not run
// at the same time because both can inspect or mutate WAL archive state.
//
// SubmitUnique is used so slow periodic jobs do not pile up behind themselves.
// For example, if an upload takes longer than one upload interval, the next
// upload tick is dropped instead of queueing stale duplicate upload work.
func (u *ArchiveSupervisor) Run(ctx context.Context, queue *jobq.JobQueue) {
	if queue == nil {
		u.log().Error("job queue is nil")
		return
	}

	uploadInterval := u.cfg.Receiver.Uploader.SyncIntervalParsed
	if uploadInterval <= 0 {
		u.log().Error("invalid upload sync interval", slog.Duration("interval", uploadInterval))
		return
	}

	uploadTicker := time.NewTicker(uploadInterval)
	defer uploadTicker.Stop()

	var retentionTicker *time.Ticker
	var retentionC <-chan time.Time

	if u.cfg.Receiver.Retention.Enable {
		retentionInterval := u.cfg.Receiver.Retention.SyncIntervalParsed
		if retentionInterval <= 0 {
			u.log().Error("invalid retention sync interval", slog.Duration("interval", retentionInterval))
			return
		}

		retentionTicker = time.NewTicker(retentionInterval)
		defer retentionTicker.Stop()

		retentionC = retentionTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting")
			return

		case <-uploadTicker.C:
			if err := queue.SubmitUnique("upload", func(ctx context.Context) {
				u.log().Debug("upload worker is running")
				defer u.log().Debug("upload worker is done")

				if err := u.performUploads(ctx); err != nil {
					u.log().Error("error uploading files", slog.Any("err", err))
				}
			}); err != nil {
				u.log().Error("error submitting upload job", slog.Any("err", err))
			}

		case <-retentionC:
			if err := queue.SubmitUnique("retain", func(ctx context.Context) {
				u.log().Debug("retention worker is running")
				defer u.log().Debug("retention worker is done")

				if err := u.performRetention(ctx, u.cfg.Receiver.Retention.KeepPeriodParsed); err != nil {
					u.log().Error("error retaining files", slog.Any("err", err))
				}
			}); err != nil {
				u.log().Error("error submitting retention job", slog.Any("err", err))
			}
		}
	}
}
