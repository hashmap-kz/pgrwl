package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	RunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()

		applyStringFallback(f, "directory", &serveOpts.Directory, "PGRWL_DIRECTORY")

		// Validate required options
		if serveOpts.Directory == "" {
			return fmt.Errorf("missing required flag: --directory or $PGRWL_DIRECTORY")
		}

		return runHTTPSrv()
	},
}

func runHTTPSrv() error {
	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// run HTTP server for managing purpose
	srv := httpsrv.NewHTTPServer(ctx, rootOpts.HTTPServerAddr, nil)
	httpsrv.Start(ctx, srv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, shutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdown()
	httpsrv.Shutdown(ctx, srv)

	return nil
}
