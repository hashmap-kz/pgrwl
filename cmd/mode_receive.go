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

// ReceiveModeOpts are the runtime parameters for receive mode, derived from
// the config file by app.go. All fields map 1-to-1 to config keys.
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// Build a restartable receiver.  It does not connect to Postgres until
	// Start() is called.
	receiver := xlog.NewRestartablePgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})

	loggr.Info("starting WAL receiver")
	if err := receiver.Start(); err != nil {
		//nolint:gocritic
		log.Fatalf("failed to start WAL receiver: %v", err)
	}

	var wg sync.WaitGroup

	// Metrics
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(ctx)
	}

	// Job queue (used by the archive supervisor and WAL deletion)
	loggr.Info("starting job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(ctx)

	// Storage - may be nil when not required
	stor := mustInitStorageIfRequired(cfg, loggr, opts)

	// HTTP server - never cancels the parent context on its own error
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
			Receiver: receiver,
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

	// Archive supervisor - only when storage is needed
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
				PGRW:             receiver,
				Verbose:          opts.Verbose,
			})
			if cfg.Receiver.Retention.Enable {
				u.RunWithRetention(ctx, jobQueue)
			} else {
				u.RunUploader(ctx, jobQueue)
			}
		}()
	}

	<-ctx.Done()
	loggr.Info("shutting down, stopping receiver...")
	receiver.Stop()

	loggr.Info("waiting for goroutines...")
	wg.Wait()
	loggr.Info("all components shut down cleanly")
}

// needSupervisorLoop reports whether the archive supervisor goroutine is
// required.  It is skipped only when using local filesystem storage without
// any compression, encryption, or retention configured - in that case the
// receiver writes final segments directly to the receive directory and no
// further processing is needed.
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
	if !needSupervisorLoop(cfg, loggr) {
		return nil
	}
	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
		BaseDir: opts.ReceiveDirectory,
		SubPath: config.LocalFSStorageSubpath,
	})
	if err != nil {
		log.Fatalf("failed to init storage: %v", err)
	}
	return stor
}
