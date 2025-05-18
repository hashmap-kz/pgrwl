package cmd

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

type ReceiveModeOpts struct {
	Directory  string
	Slot       string
	NoLoop     bool
	ListenPort int
	Verbose    bool
}

type StorageManifest struct {
	CompressionAlgo string `json:"compression_algo,omitempty"`
	EncryptionAlgo  string `json:"encryption_algo,omitempty"`
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
	pgrw, err := xlog.NewPgReceiver(ctx, &xlog.Opts{
		Directory: opts.Directory,
		Slot:      opts.Slot,
		NoLoop:    opts.NoLoop,
		Verbose:   opts.Verbose,
	})
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	var stor *storage.TransformingStorage
	if cfg.HasExternalStorageConfigured() {
		stor, err = setupStorage(opts.Directory)
		if err != nil {
			log.Fatal(err)
		}
		err := checkManifest(cfg, cfg.Mode.Receive.Directory)
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

		if err := runStreamingLoop(ctx, pgrw, opts); err != nil {
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
			BaseDir:     opts.Directory,
			Verbose:     opts.Verbose,
			RunningMode: "receive",
			Storage:     stor,
		})
		if err := runHTTPServer(ctx, opts.ListenPort, handlers); err != nil {
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
			runUploaderLoop(ctx, stor, opts.Directory, 30*time.Second)
		}()
	}

	// Wait for signal (context cancellation)
	<-ctx.Done()
	slog.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	slog.Info("all components shut down cleanly")
}

func runUploaderLoop(ctx context.Context, stor storage.Storage, dir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("(uploader-loop) context is done, exiting...")
			return
		case <-ticker.C:
			files, err := os.ReadDir(dir)
			if err != nil {
				slog.Error("error reading dir",
					slog.String("component", "uploader-loop"),
					slog.Any("err", err),
				)
				continue
			}

			for _, entry := range files {
				if entry.IsDir() {
					continue
				}
				if !xlog.IsXLogFileName(entry.Name()) {
					continue
				}

				path := filepath.ToSlash(filepath.Join(dir, entry.Name()))
				slog.Info("uploader-loop, handle file", slog.String("path", path))

				file, err := os.Open(path)
				if err != nil {
					slog.Error("error open file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
					continue
				}
				err = stor.Put(ctx, entry.Name(), file)
				if err != nil {
					slog.Error("error upload file",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
				}
				_ = file.Close()

				if err := os.Remove(path); err != nil {
					log.Printf("delete failed: %s: %v", path, err)
					slog.Error("delete failed",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
						slog.Any("err", err),
					)
				} else {
					slog.Info("uploaded and deleted",
						slog.String("component", "uploader-loop"),
						slog.String("path", path),
					)
				}
			}
		}
	}
}

func runStreamingLoop(ctx context.Context, pgrw *xlog.PgReceiveWal, opts *ReceiveModeOpts) error {
	// enter main streaming loop
	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			os.Exit(1)
		}

		select {
		case <-ctx.Done():
			slog.Info("(main) received termination signal, exiting...")
			os.Exit(0)
		default:
		}

		if opts.NoLoop {
			slog.Error("disconnected")
			os.Exit(1)
		}

		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
