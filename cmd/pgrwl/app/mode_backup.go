package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/pgrwl/pgrwl/internal/opt/api/backupmode/manualbackup"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/api/backupmode"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"
)

type BackupModeOpts struct {
	ReceiveDirectory string
}

func RunBackupMode(opts *BackupModeOpts) error {
	cfg, err := config.Cfg()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	loggr := slog.With("component", "backup-mode-runner")

	if strings.TrimSpace(cfg.Backup.Cron) == "" {
		return fmt.Errorf("backup.cron is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	fatalErrCh := make(chan error, 1)

	sendFatalErr := func(err error) {
		if err == nil {
			return
		}

		select {
		case fatalErrCh <- err:
			cancel()
		default:
			// Another fatal error was already reported.
			cancel()
		}
	}

	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	supervisor := backupsv.NewBaseBackupSupervisor(cfg, &backupsv.BaseBackupSupervisorOpts{
		Directory: opts.ReceiveDirectory,
	})

	manualSvc := manualbackup.New(manualbackup.Options{
		Gate:      supervisor,
		Directory: opts.ReceiveDirectory,
		AppCtx:    ctx,
	})

	var wg sync.WaitGroup

	//////////////////////////////////////////////////////////////////////
	// Basebackup supervisor.
	//
	// Critical component.
	// It owns cron scheduling and the shared "only one backup at a time" gate.
	//
	// Manual backup logic is intentionally separate, but uses the same gate.

	wg.Add(1)
	go func() {
		defer wg.Done()

		defer func() {
			if r := recover(); r != nil {
				sendFatalErr(fmt.Errorf("basebackup supervisor panicked: %v", r))
			}
		}()

		if err := supervisor.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			sendFatalErr(fmt.Errorf("run basebackup supervisor: %w", err))
			return
		}
	}()

	//////////////////////////////////////////////////////////////////////
	// Metrics.
	//
	// Metrics are optional.
	// The HTTP API server is not optional because backup mode exposes:
	//
	//   POST /api/v1/basebackup
	//   GET  /api/v1/basebackup
	//   GET  /api/v1/basebackup/status

	if cfg.Metrics.Enable {
		backupmetrics.InitPromMetrics(ctx)
	}

	//////////////////////////////////////////////////////////////////////
	// Backup HTTP API server.
	//
	// Non-critical component.
	// If the API port is unavailable, scheduled backups can still run.

	wg.Add(1)
	go func() {
		defer wg.Done()

		defer func() {
			if r := recover(); r != nil {
				loggr.Error("backup http server panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "backup-http-server"),
				)
			}
		}()

		srv := api.NewHTTPServer(cfg.Main.ListenPort, backupmode.Init(&backupmode.Opts{
			Cfg:        cfg,
			Controller: manualSvc,
		}))

		if err := srv.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			loggr.Error("backup http server failed", slog.Any("err", err))
		}
	}()

	//////////////////////////////////////////////////////////////////////
	// Wait for shutdown reason:
	//   - signal/context cancellation
	//   - fatal error from critical component

	var runErr error

	select {
	case <-ctx.Done():
		// Could be SIGINT/SIGTERM or cancellation caused by sendFatalErr().
		// Prefer a real fatal error if one was already sent.
		select {
		case runErr = <-fatalErrCh:
		default:
			runErr = ctx.Err()
		}

	case runErr = <-fatalErrCh:
		cancel()
	}

	loggr.Info("shutting down, waiting for goroutines...")

	wg.Wait()

	// A fatal error may have appeared while goroutines were shutting down.
	// Prefer the real component error over context.Canceled.
	select {
	case err := <-fatalErrCh:
		if err != nil {
			runErr = err
		}
	default:
	}

	if runErr == nil {
		loggr.Info("all components shut down cleanly")
		return nil
	}

	if errors.Is(runErr, context.Canceled) {
		loggr.Info("all components shut down cleanly", slog.String("reason", "shutdown requested"))
		return nil
	}

	loggr.Error("backup mode stopped with error", slog.Any("err", runErr))
	return runErr
}
