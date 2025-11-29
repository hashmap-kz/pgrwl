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

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

func RunReceiveMode(opts *receiveAPI.ReceiveModeOpts) {
	cfg := config.Cfg()
	loggr := slog.With("component", "receive-mode-runner")

	// root context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()
	defer cancel()

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	//////////////////////////////////////////////////////////////////////
	// Init WAL-receiver instance

	pgrw := mustInitPgrw(ctx, opts)

	// job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// storage (may be nil)
	stor := mustInitStorage(cfg, loggr, opts)

	//////////////////////////////////////////////////////////////////////
	// Receive pipeline service (WAL + archiver together)

	pipelineSvc := receiveAPI.NewReceivePipelineService(cfg, pgrw, stor, jobQueue, opts, loggr)

	if err := pipelineSvc.StartAsync(ctx); err != nil {
		//nolint:gocritic
		log.Fatal("failed to start pipeline service:", err)
	}
	if err := pipelineSvc.AwaitRunning(ctx); err != nil {
		log.Fatal("pipeline service didn't reach Running:", err)
	}

	// Start receiver + archiver immediately
	pipelineSvc.Resume()
	loggr.Info("receive pipeline started (wal + archiver)")

	//////////////////////////////////////////////////////////////////////
	// Init OPT components

	// metrics
	initMetrics(ctx, cfg, loggr)

	//////////////////////////////////////////////////////////////////////
	// HTTP server (it should never cancel the pipeline on errors)
	var wg sync.WaitGroup
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

		handlers := receiveAPI.Init(&receiveAPI.Opts{
			PGRW:     pgrw,
			Pipeline: pipelineSvc, // NEW: pass pipeline controller
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

	//////////////////////////////////////////////////////////////////////
	// Wait for signal (context cancellation)
	<-ctx.Done()
	loggr.Info("shutting down, waiting for goroutines...")

	// Stop pipeline (wal + archiver) first
	pipelineSvc.StopAsync()
	//nolint:errcheck
	_ = pipelineSvc.AwaitTerminated(context.Background())

	// Wait for HTTP
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

func mustInitPgrw(ctx context.Context, opts *receiveAPI.ReceiveModeOpts) xlog.PgReceiveWal {
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

func mustInitStorage(cfg *config.Config, loggr *slog.Logger, opts *receiveAPI.ReceiveModeOpts) *st.TransformingStorage {
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
