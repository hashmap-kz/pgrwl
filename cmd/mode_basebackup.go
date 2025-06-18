package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"
	"github.com/hashmap-kz/pgrwl/internal/opt/supervisors/sbackup"
)

type BackupModeOpts struct {
	ReceiveDirectory string
	Verbose          bool
}

func RunBackupMode(opts *BackupModeOpts) {
	cfg := config.Cfg()
	loggr := slog.With("component", "backup-mode-runner")

	if cfg.Backup.Cron == "" {
		loggr.Error("backup.cron is required")
		os.Exit(1)
	}

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// setup job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// metrics
	if cfg.Metrics.Enable {
		metrics.InitPromMetrics(ctx)
	}

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// BackupSupervisor
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				loggr.Error("backup loop panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "basebackup"),
				)
			}
		}()
		u := sbackup.NewBaseBackupSupervisor(cfg, &sbackup.BaseBackupSupervisorOpts{
			Directory: opts.ReceiveDirectory,
			Verbose:   opts.Verbose,
		})
		u.Run(ctx, jobQueue)
	}()

	// Wait for signal (context cancellation)
	<-ctx.Done()
	loggr.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	loggr.Info("all components shut down cleanly")
}
