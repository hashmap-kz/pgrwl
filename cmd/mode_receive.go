package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"

	"github.com/hashmap-kz/pgrwl/internal/opt/supervisor"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

type ReceiveModeOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
	ListenPort       int
	Verbose          bool
}

func RunReceiveMode(opts *ReceiveModeOpts) {
	cfg := config.Cfg()
	loggr := slog.With("component", "receive-mode-runner")

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// setup metrics
	metricsCollector := metrics.NewPgrwlMetrics(&metrics.PgrwlMetricsOpts{
		Enable: cfg.Metrics.Enable,
	})

	// setup wal-receiver
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
		Metrics:          metricsCollector,
	})
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	// TODO: config.Check() method at the boot stage
	var stor *storage.TransformingStorage
	var daysKeepRetention time.Duration
	if cfg.HasExternalStorageConfigured() {
		stor, err = supervisor.SetupStorage(opts.ReceiveDirectory)
		if err != nil {
			log.Fatal(err)
		}
		err := supervisor.CheckManifest(cfg)
		if err != nil {
			log.Fatal(err)
		}

		if cfg.Retention.Enable {
			daysKeepRetention, err = time.ParseDuration(cfg.Retention.KeepPeriod)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// main streaming loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				loggr.Error("wal-receiver panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "wal-receiver"),
				)
			}
		}()

		if err := pgrw.Run(ctx); err != nil {
			loggr.Error("streaming failed", slog.Any("err", err))
			cancel() // cancel everything on error
		}
	}()

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

		handlers := httpsrv.InitHTTPHandlers(&httpsrv.HTTPHandlersOpts{
			PGRW:        pgrw,
			BaseDir:     opts.ReceiveDirectory,
			Verbose:     opts.Verbose,
			RunningMode: config.ModeReceive,
			Storage:     stor,
		})
		srv := httpsrv.NewHTTPSrv(opts.ListenPort, handlers)
		if err := srv.Run(ctx); err != nil {
			loggr.Error("http server failed", slog.Any("err", err))
		}
	}()

	// ArchiveSupervisor
	if cfg.HasExternalStorageConfigured() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					loggr.Error("upload loop panicked",
						slog.Any("panic", r),
						slog.String("goroutine", "uploader"),
					)
				}
			}()
			u := supervisor.NewArchiveSupervisor(cfg, stor, &supervisor.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				PGRW:             pgrw,
				Metrics:          metricsCollector,
			})
			if cfg.Retention.Enable {
				u.RunWithRetention(ctx, daysKeepRetention)
			} else {
				u.RunUploader(ctx)
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
