package app

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/api"
	serveAPI "github.com/pgrwl/pgrwl/internal/opt/api/servemode"
)

type ServeModeOpts struct {
	Directory  string
	ListenPort int
}

func RunServeMode(opts *ServeModeOpts) error {
	var err error
	loggr := slog.With("component", "serve-mode-runner")

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	stor, err := api.SetupStorage(&api.SetupStorageOpts{
		BaseDir: opts.Directory,
		SubPath: config.LocalFSStorageSubpath,
	})
	if err != nil {
		return err
	}

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()

		defer func() {
			if r := recover(); r != nil {
				loggr.Info("http server panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "http-server"),
				)
			}
		}()

		handlers := serveAPI.Init(&serveAPI.Opts{
			BaseDir: opts.Directory,
			Storage: stor,
		})
		srv := api.NewHTTPServer(opts.ListenPort, handlers)
		if err := srv.Run(ctx); err != nil {
			loggr.Info("http server failed", slog.Any("err", err))
			cancel()
		}
	}()

	// Wait for signal (context cancellation)
	<-ctx.Done()
	loggr.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	loggr.Info("all components shut down cleanly")

	// TODO: errCh
	return ctx.Err()
}
