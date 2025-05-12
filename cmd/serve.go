package cmd

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"

	"github.com/spf13/cobra"
)

var serveOpts struct {
	Directory       string
	HTTPServerAddr  string
	HTTPServerToken string
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Primary flags with env fallbacks
	serveCmd.Flags().StringVarP(&serveOpts.Directory, "directory", "D", "", "Target directory (ENV: PGRWL_DIRECTORY)")

	serveCmd.Flags().StringVar(&serveOpts.HTTPServerAddr, "http-server-addr", ":5080", "Run HTTP server (ENV: PGRWL_HTTP_SERVER_ADDR)")
	serveCmd.Flags().StringVar(&serveOpts.HTTPServerToken, "http-server-token", "pgrwladmin", "HTTP server token (ENV: PGRWL_HTTP_SERVER_TOKEN)")
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

		// Check HTTP server args
		applyStringFallback(f, "http-server-addr", &serveOpts.HTTPServerAddr, "PGRWL_HTTP_SERVER_ADDR")
		applyStringFallback(f, "http-server-token", &serveOpts.HTTPServerToken, "PGRWL_HTTP_SERVER_TOKEN")
		if serveOpts.HTTPServerAddr == "" {
			log.Fatal("missing required flag: --http-server-addr or $PGRWL_HTTP_SERVER_ADDR")
		}
		if serveOpts.HTTPServerToken == "" {
			log.Fatal("missing required flag: --http-server-token or $PGRWL_HTTP_SERVER_TOKEN")
		}
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", serveOpts.HTTPServerToken)

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

		if err := runHTTPServer(ctx, serveOpts.HTTPServerAddr, httpsrv.InitHTTPHandlersStandalone(serveOpts.Directory)); err != nil {
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
