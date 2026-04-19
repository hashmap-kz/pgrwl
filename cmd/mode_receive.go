package cmd

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/pgrwl/pgrwl/internal/opt/shared/supervisor"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
	"github.com/pgrwl/pgrwl/internal/opt/modes/moderunner"
	"github.com/pgrwl/pgrwl/internal/opt/shared"
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
