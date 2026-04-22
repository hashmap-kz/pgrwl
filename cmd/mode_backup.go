package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/internal/opt/api/rest/backupmode"
	"github.com/pgrwl/pgrwl/internal/opt/api/supervisors/backupsv"

	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
	"github.com/pgrwl/pgrwl/internal/opt/shared"

	"github.com/pgrwl/pgrwl/config"
)

type BackupModeOpts struct {
	ReceiveDirectory string
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

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

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
		u := backupsv.NewBaseBackupSupervisor(cfg, &backupsv.BaseBackupSupervisorOpts{
			Directory: opts.ReceiveDirectory,
		})
		u.Run(ctx)
	}()

	// metrics
	if cfg.Metrics.Enable {
		backupmetrics.InitPromMetrics(ctx)

		// HTTP server
		// It shouldn't cancel() the main streaming loop even on error.
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					loggr.Error("http server panicked",
						slog.Any("panic", r),
						slog.String("goroutine", "http-server"),
					)
				}
			}()

			srv := shared.NewHTTPSrv(cfg.Main.ListenPort, backupmode.Init(cfg))
			if err := srv.Run(ctx); err != nil {
				loggr.Error("http server failed", slog.Any("err", err))
			}
		}()
	}

	// Wait for signal (context cancellation)
	<-ctx.Done()
	loggr.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	loggr.Info("all components shut down cleanly")
}
