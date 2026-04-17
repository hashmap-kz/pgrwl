package cmd

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	combinedAPI "github.com/pgrwl/pgrwl/internal/opt/modes/combinedmode"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesuperv"

	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
)

// CombinedModeOpts mirrors ReceiveModeOpts; the receiver starts in a
// stopped state so the operator can start it via POST /receiver.
type CombinedModeOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
	ListenPort       int
}

// RunCombinedMode starts a single process that:
//   - Exposes GET/POST /receiver to start and stop WAL streaming at runtime.
//   - Exposes GET /wal/{filename} for restore_command on the standby.
//   - Optionally runs the archive supervisor loop (same as receive mode).
func RunCombinedMode(opts *CombinedModeOpts) {
	cfg := config.Cfg()
	loggr := slog.With("component", "combined-mode-runner")

	// Process-level context: cancelled on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// Build a restartable receiver - does NOT connect to Postgres yet.
	receiver := xlog.NewRestartablePgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
	})

	loggr.Info("starting WAL receiver")
	if err := receiver.Start(); err != nil {
		loggr.Error("failed to auto-start receiver", slog.Any("err", err))
		return
	}

	var wg sync.WaitGroup

	// Metrics
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(ctx)
	}

	// Job queue (needed by supervisor)
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// Storage (optional)
	stor := mustInitStorageIfRequired(cfg, loggr, &ReceiveModeOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
	})

	// HTTP server
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

		handlers := combinedAPI.Init(&combinedAPI.HandlerOpts{
			Receiver: receiver,
			BaseDir:  opts.ReceiveDirectory,
			Storage:  stor,
		})
		srv := shared.NewHTTPSrv(opts.ListenPort, handlers)
		if err := srv.Run(ctx); err != nil {
			loggr.Error("http server failed", slog.Any("err", err))
		}
	}()

	// Archive supervisor (only when storage is needed)
	if stor != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					loggr.Error("upload loop panicked",
						slog.Any("panic", r),
						slog.String("goroutine", "wal-supervisor"),
					)
				}
			}()
			u := receivesuperv.NewArchiveSupervisor(cfg, stor, &receivesuperv.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				// The supervisor calls pgrw.CurrentOpenWALFileName() to skip
				// the currently-open segment. RestartablePgReceiver satisfies
				// PgReceiveWal, so we can pass it directly.
				PGRW: receiver,
			})
			if cfg.Receiver.Retention.Enable {
				u.RunWithRetention(ctx, jobQueue)
			} else {
				u.RunUploader(ctx, jobQueue)
			}
		}()
	}

	// Wait for shutdown
	<-ctx.Done()
	loggr.Info("shutting down, stopping receiver...")
	receiver.Stop()

	loggr.Info("waiting for goroutines...")
	wg.Wait()
	loggr.Info("all components shut down cleanly")
}
