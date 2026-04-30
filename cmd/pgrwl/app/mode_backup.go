package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	"github.com/pgrwl/pgrwl/internal/opt/api/backupmode"
	"github.com/pgrwl/pgrwl/internal/opt/metrics/backupmetrics"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"
)

type BackupModeOpts struct {
	ReceiveDirectory string
}

func RunBackupMode(opts *BackupModeOpts) error {
	cfg, err := config.Cfg()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	loggr := slog.With("component", "backup-mode-runner")

	if strings.TrimSpace(cfg.Backup.Cron) == "" {
		return fmt.Errorf("backup.cron is required")
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	loggr.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("basebackup supervisor panicked: %v", r)
			}
		}()

		u := backupsv.NewBaseBackupSupervisor(cfg, &backupsv.BaseBackupSupervisorOpts{
			Directory: opts.ReceiveDirectory,
		})

		if err := u.Run(ctx); err != nil {
			return fmt.Errorf("run basebackup supervisor: %w", err)
		}

		return nil
	})

	if cfg.Metrics.Enable {
		backupmetrics.InitPromMetrics(ctx)

		g.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					loggr.Error("metrics http server panicked",
						slog.Any("panic", r),
						slog.String("goroutine", "metrics-http-server"),
					)

					// Metrics are non-critical.
					err = nil
				}
			}()

			srv := api.NewHTTPServer(cfg.Main.ListenPort, backupmode.Init(cfg))

			if err := srv.Run(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}

				loggr.Error("metrics http server failed", slog.Any("err", err))

				// Metrics are non-critical.
				return nil
			}

			return nil
		})
	}

	err = g.Wait()

	if err == nil {
		loggr.Info("all components shut down cleanly")
		return nil
	}

	if errors.Is(err, context.Canceled) {
		loggr.Info("shutdown requested")
		return nil
	}

	loggr.Error("backup mode stopped with error", slog.Any("err", err))
	return err
}
