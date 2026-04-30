package app

import (
	"context"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	receiveAPI "github.com/pgrwl/pgrwl/internal/opt/api/receivemode"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesv"

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
}

func RunReceiveMode(opts *ReceiveModeOpts) error {
	cfg, err := config.Cfg()
	if err != nil {
		return err
	}

	loggr := slog.With("component", "receive-mode-runner")

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	//////////////////////////////////////////////////////////////////////
	// Init WAL-receiver loop first

	// setup wal-receiver
	pgrw, err := initPgrw(ctx, opts)
	if err != nil {
		return err
	}

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

	// setup storage: it may be nil
	stor, err := initStorageIfRequired(cfg, loggr, opts, pgrw)
	if err != nil {
		return err
	}

	// setup job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// setup metrics
	initMetrics(ctx, cfg, loggr)

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
		handlers := receiveAPI.Init(&receiveAPI.Opts{
			PGRW:     pgrw,
			BaseDir:  opts.ReceiveDirectory,
			Storage:  stor,
			JobQueue: jobQueue,
			Cfg:      cfg,
		})
		srv := api.NewHTTPServer(opts.ListenPort, handlers)
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
			u := receivesv.NewArchiveSupervisor(cfg, stor, &receivesv.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				PGRW:             pgrw,
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

	// TODO: errCh
	return ctx.Err()
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

func initPgrw(ctx context.Context, opts *ReceiveModeOpts) (xlog.PgReceiveWal, error) {
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
	})
	if err != nil {
		return nil, err
	}
	return pgrw, nil
}

func initStorageIfRequired(
	cfg *config.Config,
	loggr *slog.Logger,
	opts *ReceiveModeOpts,
	pgrw xlog.PgReceiveWal,
) (*st.VariadicStorage, error) {
	loggr.Info("init storage")

	var stor *st.VariadicStorage
	if needSupervisorLoop(cfg, loggr) {
		var err error

		walSegSz, err := conv.Uint64ToInt64(pgrw.WalSegSz())
		if err != nil {
			return nil, err
		}
		loggr.Info("multipart chunk part (walSegSz)", slog.Int64("sz", walSegSz))

		stor, err = api.SetupStorage(&api.SetupStorageOpts{
			BaseDir:         opts.ReceiveDirectory,
			SubPath:         config.LocalFSStorageSubpath,
			S3PartSizeBytes: walSegSz,
		})
		if err != nil {
			return nil, err
		}
	}
	return stor, nil
}
