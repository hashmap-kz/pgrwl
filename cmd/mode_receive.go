package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/internal/opt/metrics/receivemetrics"

	receiveAPI "github.com/hashmap-kz/pgrwl/internal/opt/modes/receivemode"
	"github.com/hashmap-kz/pgrwl/internal/opt/shared"

	"github.com/hashmap-kz/pgrwl/internal/opt/supervisors/receivesuperv"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
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

	//////////////////////////////////////////////////////////////////////
	// Init WAL-receiver loop first

	// setup wal-receiver
	pgrw := mustInitPgrw(ctx, opts)

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Signal channel to indicate that pgrw.Run() has started
	started := make(chan struct{})

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

		// Signal that we are starting Run()
		close(started)

		if err := pgrw.Run(ctx); err != nil {
			loggr.Error("streaming failed", slog.Any("err", err))
			cancel() // cancel everything on error
		}
	}()

	// Wait until pgrw.Run() has started
	<-started
	loggr.Info("wal-receiver started")

	//////////////////////////////////////////////////////////////////////
	// Init OPT components

	// setup job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// setup metrics
	initMetrics(ctx, cfg, loggr)

	// setup storage: it may be nil
	stor := mustInitStorageIfRequired(cfg, loggr, opts)

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
		handlers := receiveAPI.Init(&receiveAPI.ReceiveHandlerOpts{
			PGRW:     pgrw,
			BaseDir:  opts.ReceiveDirectory,
			Verbose:  opts.Verbose,
			Storage:  stor,
			JobQueue: jobQueue,
		})
		srv := shared.NewHTTPSrv(opts.ListenPort, handlers)
		if err := srv.Run(ctx); err != nil {
			loggr.Error("http server failed", slog.Any("err", err))
		}
	}()

	// ArchiveSupervisor (run this goroutine ONLY when storage is required)
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

func initMetrics(ctx context.Context, cfg *config.Config, loggr *slog.Logger) {
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(ctx)
	}
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

func mustInitPgrw(ctx context.Context, opts *ReceiveModeOpts) xlog.PgReceiveWal {
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})
	if err != nil {
		log.Fatal(err)
	}
	return pgrw
}

func mustInitStorageIfRequired(cfg *config.Config, loggr *slog.Logger, opts *ReceiveModeOpts) *st.TransformingStorage {
	var stor *st.TransformingStorage
	var err error
	if needSupervisorLoop(cfg, loggr) {
		stor, err = shared.SetupStorage(&shared.SetupStorageOpts{
			BaseDir: opts.ReceiveDirectory,
			SubPath: config.LocalFSStorageSubpath,
		})
		if err != nil {
			log.Fatal(err)
		}
		if err := shared.CheckManifest(cfg); err != nil {
			log.Fatal(err)
		}
	}
	return stor
}
