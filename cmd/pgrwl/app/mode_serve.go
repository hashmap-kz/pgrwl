package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	serveAPI "github.com/pgrwl/pgrwl/internal/opt/api/servemode"
)

type ServeModeOpts struct {
	Directory  string
	ListenPort int
}

func RunServeMode(opts *ServeModeOpts) error {
	loggr := slog.With("component", "serve-mode-runner")

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	stor, err := api.SetupStorage(&api.SetupStorageOpts{
		BaseDir: opts.Directory,
		SubPath: config.LocalFSStorageSubpath,
	})
	if err != nil {
		return fmt.Errorf("setup storage: %w", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("http server panicked: %v", r)
			}
		}()

		handlers := serveAPI.Init(&serveAPI.Opts{
			BaseDir: opts.Directory,
			Storage: stor,
		})

		srv := api.NewHTTPServer(opts.ListenPort, handlers)

		if err := srv.Run(ctx); err != nil {
			return fmt.Errorf("run http server: %w", err)
		}

		return nil
	})

	err = g.Wait()

	if err == nil {
		loggr.Info("all components shut down cleanly")
		return nil
	}

	if errors.Is(err, context.Canceled) {
		loggr.Info("shutdown requested")
		return nil
	}

	loggr.Error("serve mode stopped with error", slog.Any("err", err))
	return err
}
