package cmd

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/spf13/cobra"
)

var walReceiveOpts struct {
	Directory       string
	Slot            string
	NoLoop          bool
	HTTPServerAddr  string
	HTTPServerToken string
}

func init() {
	rootCmd.AddCommand(walReceiveCmd)

	// Primary flags with env fallbacks
	walReceiveCmd.Flags().StringVarP(&walReceiveOpts.Directory, "directory", "D", "", "Target directory (ENV: PGRWL_DIRECTORY)")
	walReceiveCmd.Flags().StringVarP(&walReceiveOpts.Slot, "slot", "S", "", "Replication slot (ENV: PGRWL_SLOT)")
	walReceiveCmd.Flags().BoolVarP(&walReceiveOpts.NoLoop, "no-loop", "n", false, "Do not reconnect (ENV: PGRWL_NO_LOOP)")

	walReceiveCmd.Flags().StringVar(&walReceiveOpts.HTTPServerAddr, "http-server-addr", ":5080", "Run HTTP server (ENV: PGRWL_HTTP_SERVER_ADDR)")
	walReceiveCmd.Flags().StringVar(&walReceiveOpts.HTTPServerToken, "http-server-token", "pgrwladmin", "HTTP server token (ENV: PGRWL_HTTP_SERVER_TOKEN)")
}

var walReceiveCmd = &cobra.Command{
	Use:   "wal-receive",
	Short: "Start the WAL receiver",
	Long: ` 
Example:
pgrwl -D /mnt/wal-archive -S bookstore_app 
`,
	SilenceUsage: true,
	Run: func(cmd *cobra.Command, _ []string) {
		f := cmd.Flags()

		applyStringFallback(f, "directory", &walReceiveOpts.Directory, "PGRWL_DIRECTORY")
		applyStringFallback(f, "slot", &walReceiveOpts.Slot, "PGRWL_SLOT")
		applyBoolFallback(f, "no-loop", &walReceiveOpts.NoLoop, "PGRWL_NO_LOOP")

		// Validate required options
		if walReceiveOpts.Directory == "" {
			log.Fatal("missing required flag: --directory or $PGRWL_DIRECTORY")
		}
		if walReceiveOpts.Slot == "" {
			log.Fatal("missing required flag: --slot or $PGRWL_SLOT")
		}

		// Check HTTP server args
		applyStringFallback(f, "http-server-addr", &walReceiveOpts.HTTPServerAddr, "PGRWL_HTTP_SERVER_ADDR")
		applyStringFallback(f, "http-server-token", &walReceiveOpts.HTTPServerToken, "PGRWL_HTTP_SERVER_TOKEN")
		if walReceiveOpts.HTTPServerAddr == "" {
			log.Fatal("missing required flag: --http-server-addr or $PGRWL_HTTP_SERVER_ADDR")
		}
		if walReceiveOpts.HTTPServerToken == "" {
			log.Fatal("missing required flag: --http-server-token or $PGRWL_HTTP_SERVER_TOKEN")
		}
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", walReceiveOpts.HTTPServerToken)

		// Validate required PG env vars
		var emptyEnvs []string
		for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD"} {
			if os.Getenv(name) == "" {
				emptyEnvs = append(emptyEnvs, name)
			}
		}
		if len(emptyEnvs) > 0 {
			log.Fatalf("required env vars are empty: [%s]", strings.Join(emptyEnvs, " "))
		}

		// Run the actual service (streaming + HTTP)
		runWalReceiver()
	},
}

func runStreamingLoop(ctx context.Context, pgrw *xlog.PgReceiveWal, opts *xlog.Opts) error {
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

func runWalReceiver() {
	opts := &xlog.Opts{
		Directory: walReceiveOpts.Directory,
		Slot:      walReceiveOpts.Slot,
		NoLoop:    walReceiveOpts.NoLoop,
	}

	// setup context
	ctx, cancel := context.WithCancel(context.Background())
	ctx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	// print options
	slog.LogAttrs(ctx, slog.LevelInfo, "opts", slog.Any("opts", opts))

	// setup wal-receiver
	pgrw, err := xlog.NewPgReceiver(ctx, opts)
	if err != nil {
		//nolint:gocritic
		log.Fatal(err)
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

		if err := runHTTPServer(ctx, walReceiveOpts.HTTPServerAddr, httpsrv.InitHTTPHandlersStreaming(pgrw)); err != nil {
			slog.Error("http server failed", slog.Any("err", err))
		}
	}()

	// Wait for signal (context cancellation)
	<-ctx.Done()
	slog.Info("shutting down, waiting for goroutines...")

	// Wait for all goroutines to finish
	wg.Wait()
	slog.Info("all components shut down cleanly")
}
