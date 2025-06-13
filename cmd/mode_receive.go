package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics"

	"github.com/hashmap-kz/pgrwl/internal/opt/supervisor"

	"github.com/hashmap-kz/pgrwl/config"

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

	// setup wal-receiver
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	var stor *st.TransformingStorage
	needSupervisorLoop := needSupervisorLoop(cfg, loggr)
	if needSupervisorLoop {
		stor, err = supervisor.SetupStorage(&supervisor.SetupStorageOpts{
			BaseDir: opts.ReceiveDirectory,
			SubPath: config.LocalFSStorageSubpath,
		})
		if err != nil {
			log.Fatal(err)
		}
		if err := supervisor.CheckManifest(cfg); err != nil {
			log.Fatal(err)
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
			JobQueue:    jobQueue,
		})
		srv := httpsrv.NewHTTPSrv(opts.ListenPort, handlers)
		if err := srv.Run(ctx); err != nil {
			loggr.Error("http server failed", slog.Any("err", err))
		}
	}()

	// ArchiveSupervisor
	if needSupervisorLoop {
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
				Verbose:          opts.Verbose,
			})
			if cfg.Receiver.Retention.Enable {
				u.RunWithRetention(ctx, jobQueue)
			} else {
				u.RunUploader(ctx, jobQueue)
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

// needSupervisorLoop decides whether we actually need to boot the storage
// we don't need if:
// * it's a localfs storage configured with no compression/encryption/retain
func needSupervisorLoop(cfg *config.Config, l *slog.Logger) bool {
	if cfg.IsLocalStor() {
		hasCfg := strings.TrimSpace(cfg.Storage.Compression.Algo) != "" ||
			strings.TrimSpace(cfg.Storage.Encryption.Algo) != "" ||
			cfg.Receiver.Retention.Enable
		if !hasCfg {
			l.Info("supervisor loop is skipped",
				slog.String("reason", "no compression/encryption or retention configs for local-storage"),
			)
		}
		return hasCfg
	}
	return true
}
