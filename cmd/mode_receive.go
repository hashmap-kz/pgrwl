package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/cmd/repo"

	"github.com/hashmap-kz/pgrwl/cmd/loops"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

type ReceiveModeOpts struct {
	ReceiveDirectory string
	StatusDirectory  string
	Slot             string
	NoLoop           bool
	ListenPort       int
	Verbose          bool
}

func RunReceiveMode(opts *ReceiveModeOpts) {
	cfg := config.Cfg()

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// print options
	slog.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// setup wal-receiver
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.PgReceiveWalOpts{
		ReceiveDirectory: opts.ReceiveDirectory,
		StatusDirectory:  opts.StatusDirectory,
		Slot:             opts.Slot,
		NoLoop:           opts.NoLoop,
		Verbose:          opts.Verbose,
	})
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	var stor *storage.TransformingStorage
	if cfg.HasExternalStorageConfigured() {
		stor, err = repo.SetupStorage(opts.ReceiveDirectory)
		if err != nil {
			log.Fatal(err)
		}
		err := repo.CheckManifest(cfg, cfg.Mode.Receive.Directory)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Use WaitGroup to wait for all goroutines to finish
	var wg sync.WaitGroup

	// main streaming loop
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("wal-receiver panicked",
					slog.Any("panic", r),
					slog.String("goroutine", "wal-receiver"),
				)
			}
		}()

		if err := pgrw.Run(ctx); err != nil {
			slog.Error("streaming failed", slog.Any("err", err))
			cancel() // cancel everything on error
		}
	}()

	// HTTP server
	// It shouldn't cancel() the main streaming loop even on error.
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
			PGRW:        pgrw,
			BaseDir:     opts.ReceiveDirectory,
			Verbose:     opts.Verbose,
			RunningMode: "receive",
			Storage:     stor,
		})
		if err := loops.RunHTTPServer(ctx, opts.ListenPort, handlers); err != nil {
			slog.Error("http server failed", slog.Any("err", err))
		}
	}()

	if cfg.HasExternalStorageConfigured() {
		// Uploader
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("upload loop panicked",
						slog.Any("panic", r),
						slog.String("goroutine", "uploader"),
					)
				}
			}()
			u := loops.NewUploader(cfg, stor, &loops.UploaderLoopOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				StatusDirectory:  opts.StatusDirectory,
			})
			u.Run(ctx)
		}()
	}

	// Wait for signal (context cancellation)
	<-ctx.Done()
	slog.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	slog.Info("all components shut down cleanly")
}
