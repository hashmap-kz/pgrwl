package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

type ServeModeOpts struct {
	Directory  string
	ListenPort int
	Verbose    bool
}

func RunServeMode(opts *ServeModeOpts) {
	cfg := config.Cfg()
	var err error

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	var stor *storage.TransformingStorage
	if cfg.HasExternalStorageConfigured() {
		stor, err = setupStorage(opts.Directory)
		if err != nil {
			log.Fatal(err)
		}
		manifest, err := checkStorageManifest(cfg, cfg.Mode.Serve.Directory)
		if err != nil {
			log.Fatal(err)
		}
		if manifest.CompressionAlgo != cfg.Storage.Compression.Algo {
			log.Fatal("storage compression mismatch from previous setup")
		}
		if manifest.EncryptionAlgo != cfg.Storage.Encryption.Algo {
			log.Fatal("storage encryption mismatch from previous setup")
		}
	}

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

		handlers := httpsrv.InitHTTPHandlers(&httpsrv.HTTPHandlersOpts{
			BaseDir:     opts.Directory,
			Verbose:     opts.Verbose,
			RunningMode: "serve",
			Storage:     stor,
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
