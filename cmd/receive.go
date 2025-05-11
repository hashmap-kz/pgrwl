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

var receiveOpts struct {
	Directory       string
	Slot            string
	NoLoop          bool
	LogLevel        string
	LogFormat       string
	LogAddSource    bool
	HTTPServerAddr  string
	HTTPServerToken string
}

func init() {
	rootCmd.AddCommand(receiveCmd)

	// Primary flags with env fallbacks
	receiveCmd.Flags().StringVarP(&receiveOpts.Directory, "directory", "D", "", "Target directory (ENV: PGRWL_DIRECTORY)")
	receiveCmd.Flags().StringVarP(&receiveOpts.Slot, "slot", "S", "", "Replication slot (ENV: PGRWL_SLOT)")
	receiveCmd.Flags().BoolVarP(&receiveOpts.NoLoop, "no-loop", "n", false, "Do not reconnect (ENV: PGRWL_NO_LOOP)")

	// Logging
	receiveCmd.Flags().StringVar(&receiveOpts.LogLevel, "log-level", "", "Log level (ENV: PGRWL_LOG_LEVEL)")
	receiveCmd.Flags().StringVar(&receiveOpts.LogFormat, "log-format", "", "Log format (ENV: PGRWL_LOG_FORMAT)")
	receiveCmd.Flags().BoolVar(&receiveOpts.LogAddSource, "log-add-source", false, "Include source info in logs (ENV: PGRWL_LOG_ADD_SOURCE)")

	// Optional HTTP server
	receiveCmd.Flags().StringVar(&receiveOpts.HTTPServerAddr, "http-server-addr", "", "Run HTTP server (ENV: PGRWL_HTTP_SERVER_ADDR)")
	receiveCmd.Flags().StringVar(&receiveOpts.HTTPServerToken, "http-server-token", "", "HTTP server token (ENV: PGRWL_HTTP_SERVER_TOKEN)")
}

var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "Start the WAL receiver",
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()

		applyStringFallback(f, "directory", &receiveOpts.Directory, "PGRWL_DIRECTORY")
		applyStringFallback(f, "slot", &receiveOpts.Slot, "PGRWL_SLOT")
		applyBoolFallback(f, "no-loop", &receiveOpts.NoLoop, "PGRWL_NO_LOOP")

		applyStringFallback(f, "log-level", &receiveOpts.LogLevel, "PGRWL_LOG_LEVEL")
		applyStringFallback(f, "log-format", &receiveOpts.LogFormat, "PGRWL_LOG_FORMAT")
		applyBoolFallback(f, "log-add-source", &receiveOpts.LogAddSource, "PGRWL_LOG_ADD_SOURCE")

		applyStringFallback(f, "http-server-addr", &receiveOpts.HTTPServerAddr, "PGRWL_HTTP_SERVER_ADDR")
		applyStringFallback(f, "http-server-token", &receiveOpts.HTTPServerToken, "PGRWL_HTTP_SERVER_TOKEN")

		// Validate required options
		if receiveOpts.Directory == "" {
			return fmt.Errorf("missing required flag: --directory or $PGRWL_DIRECTORY")
		}
		if receiveOpts.Slot == "" {
			return fmt.Errorf("missing required flag: --slot or $PGRWL_SLOT")
		}

		// Validate required PG env vars
		for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD"} {
			if os.Getenv(name) == "" {
				return fmt.Errorf("required env var %s is not set", name)
			}
		}

		// Set log envs
		_ = os.Setenv("LOG_LEVEL", receiveOpts.LogLevel)
		_ = os.Setenv("LOG_FORMAT", receiveOpts.LogFormat)
		if receiveOpts.LogAddSource {
			_ = os.Setenv("LOG_ADD_SOURCE", "1")
		}

		// Run the actual service (streaming + HTTP)
		return runService() // you'll implement this
	},
}

func runService() error {
	opts := &xlog.Opts{
		Directory: receiveOpts.Directory,
		Slot:      receiveOpts.Slot,
		NoLoop:    receiveOpts.NoLoop,
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
	if receiveOpts.HTTPServerAddr != "" {
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", receiveOpts.HTTPServerToken)
		srv = httpsrv.NewHTTPServer(ctx, receiveOpts.HTTPServerAddr, pgrw)
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
