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

	// Root context for the whole program (http, job queue, etc.)
	rootCtx, rootCancel := context.WithCancel(context.Background())
	rootCtx, signalCancel := signal.NotifyContext(rootCtx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()
	defer rootCancel()

	loggr.LogAttrs(rootCtx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// Init WAL receiver instance (no streaming yet)
	pgrw, err := xlog.NewPgReceiver(rootCtx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	var wg sync.WaitGroup

	// Create controller and start streaming
	streamCtl := receiveAPI.NewStreamController(rootCtx, loggr, pgrw)
	streamCtl.Start(&wg)

	//////////////////////////////////////////////////////////////////////
	// Init OPT components

	// setup job queue
	loggr.Info("running job queue")
	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(rootCtx)

	// metrics
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(rootCtx)
	}

	var stor *st.TransformingStorage
	needSupervisorLoop := needSupervisorLoop(cfg, loggr)
	if needSupervisorLoop {
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
			PGRW:             pgrw,
			StreamController: streamCtl,
			WG:               &wg,
			BaseDir:          opts.ReceiveDirectory,
			Verbose:          opts.Verbose,
			Storage:          stor,
			JobQueue:         jobQueue,
		})
		srv := shared.NewHTTPSrv(opts.ListenPort, handlers)
		if err := srv.Run(rootCtx); err != nil {
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
				u.RunWithRetention(rootCtx, jobQueue)
			} else {
				u.RunUploader(rootCtx, jobQueue)
			}
		}()
	}

	// Wait for signal (context cancellation)
	<-rootCtx.Done()
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
