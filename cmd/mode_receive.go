package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"

	receiveAPI "github.com/pgrwl/pgrwl/internal/opt/modes/receivemode"
	"github.com/pgrwl/pgrwl/internal/opt/shared"

	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesuperv"

	"github.com/pgrwl/pgrwl/internal/opt/jobq"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"

	"github.com/pgrwl/pgrwl/config"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
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
	defer cancel()
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	//////////////////////////////////////////////////////////////////////
	// Init WAL-receiver

	// Build a restartable receiver. It does not connect to Postgres until
	// Start() is called.
	pgrw := xlog.NewRestartablePgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})

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

	// ArchiveSupervisor runs as a Worker inside the receiver's start/stop
	// lifecycle. When the receiver is stopped via the API, the archiver is
	// also cancelled. When the receiver is restarted, the archiver restarts
	// with it on the new child context.
	if stor != nil {
		u := receivesuperv.NewArchiveSupervisor(cfg, stor, &receivesuperv.ArchiveSupervisorOpts{
			ReceiveDirectory: opts.ReceiveDirectory,
			PGRW:             pgrw,
			Verbose:          opts.Verbose,
		})
		if cfg.Receiver.Retention.Enable {
			pgrw.AddWorker("archive-supervisor", func(ctx context.Context) {
				u.RunWithRetention(ctx, jobQueue)
			})
		} else {
			pgrw.AddWorker("archive-supervisor", func(ctx context.Context) {
				u.RunUploader(ctx, jobQueue)
			})
		}
	}

	loggr.Info("starting WAL receiver")
	if err := pgrw.Start(); err != nil {
		loggr.Error("failed to start WAL receiver", slog.Any("err", err))
		cancel()
		return
	}

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

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
			Receiver: pgrw,
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

	// Wait for signal (context cancellation)
	<-ctx.Done()
	loggr.Info("shutting down, stopping receiver...")
	pgrw.Stop()

	loggr.Info("waiting for goroutines...")
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

func mustInitStorageIfRequired(cfg *config.Config, loggr *slog.Logger, opts *ReceiveModeOpts) *st.VariadicStorage {
	var stor *st.VariadicStorage
	var err error
	if needSupervisorLoop(cfg, loggr) {
		stor, err = shared.SetupStorage(&shared.SetupStorageOpts{
			BaseDir: opts.ReceiveDirectory,
			SubPath: config.LocalFSStorageSubpath,
		})
		if err != nil {
			log.Fatal(err)
		}
	}
	return stor
}
