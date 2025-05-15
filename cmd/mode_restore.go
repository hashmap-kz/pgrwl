package cmd

import (
	"context"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

type RestoreModeOpts struct {
	Directory  string
	ListenPort int
}

func RunRestoreMode(opts *RestoreModeOpts) {
	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// HTTP server
	wg.Add(1)
	go func() {
		defer wg.Done()

		defer func() {
			if r := recover(); r != nil {
				slog.Error("http server panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "http-server"),
				)
			}
		}()

		handlers := httpsrv.InitHTTPHandlers(&httpsrv.HTTPHandlersDeps{
			BaseDir: opts.Directory,
		})
		if err := runHTTPServer(ctx, opts.ListenPort, handlers); err != nil {
			slog.Error("http server failed", slog.Any("err", err))
			cancel()
		}
	}()

	// Wait for signal (context cancellation)
	<-ctx.Done()
	slog.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	slog.Info("all components shut down cleanly")
}
