package main

import (
	"context"
	"fmt"
	"github.com/hashmap-kz/pgrwl/internal/utils"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/logger"
	"github.com/hashmap-kz/pgrwl/internal/xlog"

	"github.com/jackc/pgx/v5/pgconn"
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

	// setup wal-receiver
	pgrw := setupPgReceiver(ctx, opts)

	// enter main streaming loop
	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			//nolint:gocritic
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

func setupPgReceiver(ctx context.Context, opts *utils.Opts) *xlog.PgReceiveWal {
	connStrRepl := fmt.Sprintf("application_name=%s replication=yes", opts.Slot)
	conn, err := pgconn.Connect(ctx, connStrRepl)
	if err != nil {
		slog.Error("cannot establish connection", slog.Any("err", err))
		os.Exit(1)
	}
	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		log.Fatal(err)
	}
	return &xlog.PgReceiveWal{
		BaseDir:     opts.Directory,
		WalSegSz:    startupInfo.WalSegSz,
		Conn:        conn,
		ConnStrRepl: connStrRepl,
		SlotName:    opts.Slot,
		// To prevent log-attributes evaluation, and fully eliminate function calls for non-trace levels
		Verbose: strings.EqualFold(os.Getenv("LOG_LEVEL"), "trace"),
	}
}
