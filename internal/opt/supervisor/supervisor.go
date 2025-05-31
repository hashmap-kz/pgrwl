package supervisor

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/jobq"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

type ArchiveSupervisorOpts struct {
	ReceiveDirectory string
	PGRW             xlog.PgReceiveWal
	Verbose          bool
}

type uploadBundle struct {
	walFilePath string
}

type ArchiveSupervisor struct {
	l       *slog.Logger
	mu      sync.Mutex
	cfg     *config.Config
	stor    storage.Storage
	opts    *ArchiveSupervisorOpts
	verbose bool

	// opts (for fast-access without traverse the config)
	storageName string
}

func NewArchiveSupervisor(cfg *config.Config, stor storage.Storage, opts *ArchiveSupervisorOpts) *ArchiveSupervisor {
	return &ArchiveSupervisor{
		l:           slog.With(slog.String("component", "archive-supervisor")),
		cfg:         cfg,
		stor:        stor,
		opts:        opts,
		verbose:     opts.Verbose,
		storageName: cfg.Storage.Name,
	}
}

func (u *ArchiveSupervisor) log() *slog.Logger {
	if u.l != nil {
		return u.l
	}
	return slog.With(slog.String("component", "archive-supervisor"))
}

func (u *ArchiveSupervisor) RunUploader(ctx context.Context, queue *jobq.JobQueue) {
	ticker := time.NewTicker(u.cfg.Storage.Uploader.SyncIntervalParsed)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting...")
			return
		case <-ticker.C:
			queue.Submit("upload", func(ctx context.Context) {
				u.log().Debug("upload worker is running")
				u.mu.Lock()
				defer u.mu.Unlock()
				err := u.performUploads(ctx)
				if err != nil {
					u.log().Error("error upload files", slog.Any("err", err))
				}
				u.log().Debug("upload worker is done")
			})
		}
	}
}

func (u *ArchiveSupervisor) RunWithRetention(ctx context.Context, queue *jobq.JobQueue) {
	uploadTicker := time.NewTicker(u.cfg.Storage.Uploader.SyncIntervalParsed)
	retentionTicker := time.NewTicker(u.cfg.Storage.Retention.SyncIntervalParsed)
	defer uploadTicker.Stop()
	defer retentionTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			u.log().Info("context is done, exiting...")
			return
		case <-uploadTicker.C:
			queue.Submit("upload", func(ctx context.Context) {
				u.log().Debug("upload worker is running")
				u.mu.Lock()
				defer u.mu.Unlock()
				err := u.performUploads(ctx)
				if err != nil {
					u.log().Error("error upload files", slog.Any("err", err))
				}
				u.log().Debug("upload worker is done")
			})
		case <-retentionTicker.C:
			queue.Submit("retain", func(ctx context.Context) {
				u.log().Debug("retention worker is running")
				u.mu.Lock()
				defer u.mu.Unlock()
				err := u.performRetention(ctx, u.cfg.Storage.Retention.KeepPeriodParsed)
				if err != nil {
					u.log().Error("error retain files", slog.Any("err", err))
				}
				u.log().Debug("retention worker is done")
			})
		}
	}
}
