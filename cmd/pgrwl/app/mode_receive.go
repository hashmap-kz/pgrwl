package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	receiveAPI "github.com/pgrwl/pgrwl/internal/opt/api/receivemode"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesv"
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
		return fmt.Errorf("load config: %w", err)
	}

	loggr := slog.With("component", "receive-mode-runner")

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// fatalErrCh is used only by critical components.
	//
	// Critical:
	//   - WAL receiver
	//   - archive supervisor, when enabled
	//
	// Non-critical:
	//   - HTTP API
	//   - metrics
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

	// print options
	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	//////////////////////////////////////////////////////////////////////
	// Init WAL-receiver first

	pgrw, err := initPgrw(ctx, opts)
	if err != nil {
		return fmt.Errorf("init wal receiver: %w", err)
	}

	//////////////////////////////////////////////////////////////////////
	// Init OPT components before starting goroutines.
	//
	// This is safer than starting pgrw.Run(ctx) first and then failing later
	// during storage setup.

	stor, err := initStorageIfRequired(cfg, loggr, opts, pgrw)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}

	// setup job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// setup metrics
	initMetrics(ctx, cfg, loggr)

	var wg sync.WaitGroup

	//////////////////////////////////////////////////////////////////////
	// Main WAL receiver loop.
	//
	// This is the core component. Any error or panic is fatal.

	wg.Add(1)
	go func() {
		defer wg.Done()

		defer func() {
			if r := recover(); r != nil {
				sendFatalErr(fmt.Errorf("wal receiver panicked: %v", r))
			}
		}()

		loggr.Info("wal-receiver started")

		if err := pgrw.Run(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}

			sendFatalErr(fmt.Errorf("streaming failed: %w", err))
			return
		}

		loggr.Info("wal-receiver stopped")
	}()

	//////////////////////////////////////////////////////////////////////
	// HTTP server.
	//
	// Non-critical. It should not cancel the main streaming loop.

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
			if errors.Is(err, context.Canceled) {
				return
			}

			loggr.Error("http server failed", slog.Any("err", err))
		}
	}()

	//////////////////////////////////////////////////////////////////////
	// ArchiveSupervisor.
	//
	// Run this goroutine only when storage is required.
	//
	// This assumes ArchiveSupervisor has the new single method:
	//
	//     Run(ctx context.Context, queue *jobq.JobQueue) error
	//
	// Temporary upload/retention errors should be logged inside Run() and
	// retried on the next tick. Only structural/fatal supervisor errors should
	// be returned from Run().

	if stor != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					sendFatalErr(fmt.Errorf("wal archive supervisor panicked: %v", r))
				}
			}()

			u := receivesv.NewArchiveSupervisor(cfg, stor, &receivesv.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				PGRW:             pgrw,
			})

			if err := u.Run(ctx, jobQueue); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}

				sendFatalErr(fmt.Errorf("run wal archive supervisor: %w", err))
				return
			}
		}()
	}

	//////////////////////////////////////////////////////////////////////
	// Wait for shutdown reason:
	//   - signal/context cancellation
	//   - fatal error from critical component

	var runErr error

	select {
	case <-ctx.Done():
		// Could be SIGINT/SIGTERM or cancellation caused by sendFatalErr().
		// Try to prefer real fatal error if one was sent.
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

	loggr.Error("receive mode stopped with error", slog.Any("err", runErr))
	return runErr
}

func initMetrics(ctx context.Context, cfg *config.Config, loggr *slog.Logger) {
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(ctx)
	}
}

// needSupervisorLoop decides whether we actually need to boot the storage.
//
// We don't need it if:
//   - storage is localfs
//   - no compression is configured
//   - no encryption is configured
//   - receiver retention is disabled
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
		walSegSz, err := conv.Uint64ToInt64(pgrw.WalSegSz())
		if err != nil {
			return nil, fmt.Errorf("convert wal segment size: %w", err)
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
