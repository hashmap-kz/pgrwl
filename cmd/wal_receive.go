package cmd

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"

	"github.com/spf13/cobra"
)

var walReceiveOpts struct {
	Directory string
	Slot      string
	NoLoop    bool
}

func init() {
	rootCmd.AddCommand(walReceiveCmd)

	// Primary flags with env fallbacks
	walReceiveCmd.Flags().StringVarP(&walReceiveOpts.Directory, "directory", "D", "", "Target directory (ENV: PGRWL_DIRECTORY)")
	walReceiveCmd.Flags().StringVarP(&walReceiveOpts.Slot, "slot", "S", "", "Replication slot (ENV: PGRWL_SLOT)")
	walReceiveCmd.Flags().BoolVarP(&walReceiveOpts.NoLoop, "no-loop", "n", false, "Do not reconnect (ENV: PGRWL_NO_LOOP)")
}

var walReceiveCmd = &cobra.Command{
	Use:   "wal-receive",
	Short: "Start the WAL receiver",
	Long: ` 
Example:
pgrwl -D /mnt/wal-archive -S bookstore_app 
`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()

		applyStringFallback(f, "directory", &walReceiveOpts.Directory, "PGRWL_DIRECTORY")
		applyStringFallback(f, "slot", &walReceiveOpts.Slot, "PGRWL_SLOT")
		applyBoolFallback(f, "no-loop", &walReceiveOpts.NoLoop, "PGRWL_NO_LOOP")

		// Validate required options
		if walReceiveOpts.Directory == "" {
			return fmt.Errorf("missing required flag: --directory or $PGRWL_DIRECTORY")
		}
		if walReceiveOpts.Slot == "" {
			return fmt.Errorf("missing required flag: --slot or $PGRWL_SLOT")
		}

		// Validate required PG env vars
		for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD"} {
			if os.Getenv(name) == "" {
				return fmt.Errorf("required env var %s is not set", name)
			}
		}

		// Run the actual service (streaming + HTTP)
		return runWalReceiver()
	},
}

func runWalReceiver() error {
	opts := &xlog.Opts{
		Directory: walReceiveOpts.Directory,
		Slot:      walReceiveOpts.Slot,
		NoLoop:    walReceiveOpts.NoLoop,
	}

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Init()

	// print options
	slog.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// setup wal-receiver
	pgrw, err := xlog.NewPgReceiver(ctx, opts)
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
	}

	// optionally run HTTP server for managing purpose
	var srv *httpsrv.HTTPServer
	if rootOpts.HTTPServerAddr != "" {
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", rootOpts.HTTPServerToken)
		srv = httpsrv.NewHTTPServer(ctx, rootOpts.HTTPServerAddr, pgrw)
		httpsrv.Start(ctx, srv)
	}

	// enter main streaming loop
	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			httpsrv.Shutdown(ctx, srv)
			os.Exit(1)
		}

		select {
		case <-ctx.Done():
			slog.Info("(main) received termination signal, exiting...")
			httpsrv.Shutdown(ctx, srv)
			os.Exit(0)
		default:
		}

		if opts.NoLoop {
			slog.Error("disconnected")
			httpsrv.Shutdown(ctx, srv)
			os.Exit(1)
		}

		pgrw.SetStream(nil)
		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
