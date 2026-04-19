package cmd

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pgrwl/pgrwl/internal/opt/shared/supervisor"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/conv"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
	"github.com/pgrwl/pgrwl/internal/opt/modes/moderunner"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type ReceiveModeOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
	ListenPort       int
}

func RunReceiveMode(opts *ReceiveModeOpts) {
	cfg := config.Cfg()
	loggr := slog.With("component", "receive-mode-runner")

	ctx, signalCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	initMetrics(ctx, cfg, loggr)

	// Build the swappable router — placeholder until the first Switch call.
	modeRouter := moderunner.NewModeRouter(http.NotFoundHandler())

	// Top-level mux: permanent routes + mode-delegating catch-all.
	topMux := http.NewServeMux()
	topMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	shared.InitOptionalHandlers(cfg, topMux, loggr)

	mgr := moderunner.NewManager(
		ctx,
		&moderunner.ManagerOpts{
			ReceiveDirectory: opts.ReceiveDirectory,
			Slot:             opts.Slot,
			NoLoop:           opts.NoLoop,
		},
		cfg,
		modeRouter,
		moderunner.ManagerDeps{
			InitPgrw: func() xlog.PgReceiveWal {
				return mustInitPgrw(ctx, opts)
			},
			InitReceiveStorage: func(pgrw xlog.PgReceiveWal) *st.VariadicStorage {
				return mustInitReceiveStorage(cfg, loggr, opts, pgrw)
			},
			InitServeStorage: func() *st.VariadicStorage {
				return mustInitServeStorage(loggr, opts)
			},
		},
	)

	moderunner.MountModeRoutes(topMux, mgr)
	topMux.Handle("/", modeRouter)

	// HTTP server runs for the lifetime of the process.
	httpSup := supervisor.New(loggr)
	if err := httpSup.RegisterCritical("http-server", func(ctx context.Context) error {
		return shared.NewHTTPSrv(opts.ListenPort, topMux).Run(ctx)
	}); err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}
	if err := httpSup.Start(ctx); err != nil {
		log.Fatal(err)
	}

	// Default mode is receive.
	if err := mgr.Switch(config.ModeReceive); err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()
	loggr.Info("shutting down...")

	managerStopCtx, managerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer managerCancel()
	if err := mgr.Stop(managerStopCtx); err != nil {
		loggr.Error("failed to stop mode manager", slog.Any("err", err))
	}

	httpStopCtx, httpCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer httpCancel()
	if err := httpSup.Stop(httpStopCtx); err != nil {
		loggr.Error("failed to stop http supervisor", slog.Any("err", err))
	}

	loggr.Info("all components shut down cleanly")
}

func initMetrics(ctx context.Context, cfg *config.Config, loggr *slog.Logger) {
	if cfg.Metrics.Enable {
		loggr.Debug("init prom metrics")
		receivemetrics.InitPromMetrics(ctx)
	}
}

// needSupervisorLoop decides whether we need to boot the storage supervisor.
// Skipped for local-fs with no compression, encryption, or retention configured.
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
	})
	if err != nil {
		log.Fatal(err)
	}
	return pgrw
}

// mustInitReceiveStorage sets up storage for receive mode.
// S3PartSizeBytes is set to walSegSz so each WAL segment is uploaded as a single part.
func mustInitReceiveStorage(cfg *config.Config, loggr *slog.Logger, opts *ReceiveModeOpts, pgrw xlog.PgReceiveWal) *st.VariadicStorage {
	loggr.Info("init receive storage")

	if !needSupervisorLoop(cfg, loggr) {
		return nil
	}

	walSegSz, err := conv.Uint64ToInt64(pgrw.WalSegSz())
	if err != nil {
		log.Fatal(err)
	}
	loggr.Info("multipart chunk part (walSegSz)", slog.Int64("sz", walSegSz))

	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
		BaseDir:         opts.ReceiveDirectory,
		SubPath:         config.LocalFSStorageSubpath,
		S3PartSizeBytes: walSegSz,
	})
	if err != nil {
		log.Fatal(err)
	}
	return stor
}

// mustInitServeStorage sets up storage for serve mode.
// No pgrw needed — S3PartSizeBytes is 0, letting the S3 backend use its own default.
func mustInitServeStorage(loggr *slog.Logger, opts *ReceiveModeOpts) *st.VariadicStorage {
	loggr.Info("init serve storage")

	stor, err := shared.SetupStorage(&shared.SetupStorageOpts{
		BaseDir: opts.ReceiveDirectory,
		SubPath: config.LocalFSStorageSubpath,
	})
	if err != nil {
		log.Fatal(err)
	}
	return stor
}
