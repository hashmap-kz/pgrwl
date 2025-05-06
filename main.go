package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pgreceivewal5/internal/logger"
	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pgx/v5/pgconn"
)

type Opts struct {
	Directory    string
	Slot         string
	NoLoop       bool
	LogLevel     string
	LogAddSource bool
}

func main() {
	// parse CLI (it sets env-vars, checks required args, so it's need to be executed at the top)
	opts := parseFlags()

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Init()

	// setup wal-receiver
	pgrw := setupPgReceiver(opts, ctx)

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

func parseFlags() Opts {
	opts := Opts{}

	flag.StringVar(&opts.Directory, "D", "", "")
	flag.StringVar(&opts.Directory, "directory", "", "")
	flag.StringVar(&opts.Slot, "S", "", "")
	flag.StringVar(&opts.Slot, "slot", "", "")
	flag.BoolVar(&opts.NoLoop, "n", false, "")
	flag.BoolVar(&opts.NoLoop, "no-loop", false, "")
	flag.StringVar(&opts.LogLevel, "log-level", "info", "")
	flag.BoolVar(&opts.LogAddSource, "log-add-source", false, "")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, `Usage: x05 [OPTIONS]

Main Options:
  -D, --directory         receive write-ahead log files into this directory (required)
  -S, --slot              replication slot to use (required)
  -n, --no-loop           do not loop on connection lost
      --log-level         set log level (e.g., trace, debug, info, warn, error) (default: info)
      --log-add-source    include source file and line in log output (default: false)
`)
	}
	flag.Parse()

	if opts.Directory == "" {
		log.Fatal("directory is not specified")
	}
	if opts.Slot == "" {
		log.Fatal("replication slot name is not specified")
	}

	// set env-vars
	_ = os.Setenv("LOG_LEVEL", opts.LogLevel)
	if opts.LogAddSource {
		_ = os.Setenv("LOG_ADD_SOURCE", "1")
	}

	// check connstr vars are set

	requiredVars := []string{
		"PGHOST",
		"PGPORT",
		"PGUSER",
		"PGPASSWORD",
	}
	var empty []string
	for _, v := range requiredVars {
		if os.Getenv(v) == "" {
			empty = append(empty, v)
		}
	}
	if len(empty) > 0 {
		log.Fatalf("required vars are empty: [%s]", strings.Join(empty, " "))
	}
	return opts
}

func setupPgReceiver(opts Opts, ctx context.Context) *xlog.PgReceiveWal {
	connStrRepl := fmt.Sprintf("application_name=%s replication=yes", opts.Slot)
	conn, err := pgconn.Connect(ctx, connStrRepl)
	if err != nil {
		slog.Error("cannot establish connection", slog.Any("err", err))
		//nolint:gocritic
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
