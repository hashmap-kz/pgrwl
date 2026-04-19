package receivesuperv

import (
	"context"
	"log/slog"
	"time"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
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

	// opts (for fast-access without traverse the config)
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
			if err := queue.Submit("upload", func(ctx context.Context) {
				u.log().Debug("upload worker is running")
				defer u.log().Debug("upload worker is done")

				if err := u.performUploads(ctx); err != nil {
					u.log().Error("error uploading files", slog.Any("err", err))
				}
			}); err != nil {
				u.log().Error("error submitting upload job", slog.Any("err", err))
			}

		case <-retentionC:
			if err := queue.Submit("retain", func(ctx context.Context) {
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

// func (u *ArchiveSupervisor) RunUploader(ctx context.Context, queue *jobq.JobQueue) {
// 	ticker := time.NewTicker(u.cfg.Receiver.Uploader.SyncIntervalParsed)
// 	defer ticker.Stop()
//
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			u.log().Info("context is done, exiting...")
// 			return
// 		case <-ticker.C:
// 			err := queue.Submit("upload", func(ctx context.Context) {
// 				u.log().Debug("upload worker is running")
// 				err := u.performUploads(ctx)
// 				if err != nil {
// 					u.log().Error("error upload files", slog.Any("err", err))
// 				}
// 				u.log().Debug("upload worker is done")
// 			})
// 			if err != nil {
// 				u.log().Error("error submit upload files job", slog.Any("err", err))
// 			}
// 		}
// 	}
// }
//
// func (u *ArchiveSupervisor) RunWithRetention(ctx context.Context, queue *jobq.JobQueue) {
// 	uploadTicker := time.NewTicker(u.cfg.Receiver.Uploader.SyncIntervalParsed)
// 	retentionTicker := time.NewTicker(u.cfg.Receiver.Retention.SyncIntervalParsed)
// 	defer uploadTicker.Stop()
// 	defer retentionTicker.Stop()
//
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			u.log().Info("context is done, exiting...")
// 			return
// 		case <-uploadTicker.C:
// 			err := queue.Submit("upload", func(ctx context.Context) {
// 				u.log().Debug("upload worker is running")
// 				err := u.performUploads(ctx)
// 				if err != nil {
// 					u.log().Error("error upload files", slog.Any("err", err))
// 				}
// 				u.log().Debug("upload worker is done")
// 			})
// 			if err != nil {
// 				u.log().Error("error submit upload files job", slog.Any("err", err))
// 			}
// 		case <-retentionTicker.C:
// 			err := queue.Submit("retain", func(ctx context.Context) {
// 				u.log().Debug("retention worker is running")
// 				err := u.performRetention(ctx, u.cfg.Receiver.Retention.KeepPeriodParsed)
// 				if err != nil {
// 					u.log().Error("error retain files", slog.Any("err", err))
// 				}
// 				u.log().Debug("retention worker is done")
// 			})
// 			if err != nil {
// 				u.log().Error("error submit retain files job", slog.Any("err", err))
// 			}
// 		}
// 	}
// }
