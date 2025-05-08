package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/coreutils"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/httpsrv"
)

func main() {
	// parse CLI (it sets env-vars, checks required args, so it's need to be executed at the top)
	opts, err := coreutils.ParseFlags()
	if err != nil {
		log.Fatal(err)
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
	if opts.HTTPServerAddr != "" {
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", opts.HTTPServerToken)
		srv = httpsrv.NewHTTPServer(ctx, opts.HTTPServerAddr, pgrw)
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
