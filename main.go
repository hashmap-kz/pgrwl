package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/httpsrv"

	"github.com/hashmap-kz/pgrwl/internal/utils"

	"github.com/hashmap-kz/pgrwl/internal/logger"
	"github.com/hashmap-kz/pgrwl/internal/xlog"
)

func main() {
	// parse CLI (it sets env-vars, checks required args, so it's need to be executed at the top)
	opts, err := utils.ParseFlags()
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

	// TODO: should be optional
	// managing
	httpSrv := httpsrv.NewHTTPServer(ctx, ":8080", pgrw)
	httpSrv.Start(ctx)

	// enter main streaming loop
	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			httpSrv.Shutdown(ctx)
			os.Exit(1)
		}

		select {
		case <-ctx.Done():
			slog.Info("(main) received termination signal, exiting...")
			httpSrv.Shutdown(ctx)
			os.Exit(0)
		default:
		}

		if opts.NoLoop {
			slog.Error("disconnected")
			httpSrv.Shutdown(ctx)
			os.Exit(1)
		}

		pgrw.SetStream(nil)
		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
