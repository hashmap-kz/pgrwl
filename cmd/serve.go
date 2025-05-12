package cmd

import (
	"context"
	"log"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"

	"github.com/spf13/cobra"
)

var serveOpts struct {
	Directory string
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Primary flags with env fallbacks
	serveCmd.Flags().StringVarP(&serveOpts.Directory, "directory", "D", "", "Target directory (ENV: PGRWL_DIRECTORY)")
}

var serveCmd = &cobra.Command{
	Use:          "serve",
	Short:        "Start HTTP server (for restore ops)",
	SilenceUsage: true,
	Run: func(cmd *cobra.Command, _ []string) {
		f := cmd.Flags()

		applyStringFallback(f, "directory", &serveOpts.Directory, "PGRWL_DIRECTORY")

		// Validate required options
		if serveOpts.Directory == "" {
			log.Fatal("missing required flag: --directory or $PGRWL_DIRECTORY")
		}

		runHTTPSrv()
	},
}

func runHTTPSrv() {
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

		if err := runHTTPServer(ctx, httpsrv.InitHTTPHandlersStandalone(serveOpts.Directory)); err != nil {
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
