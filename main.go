package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
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

// socket for managing +

type Command struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

func startTCPListener(ctx context.Context, addr string, cancel context.CancelFunc) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("failed to listen", slog.Any("err", err))
		os.Exit(1)
	}
	defer l.Close()
	slog.Info("listening for commands", slog.String("addr", addr))

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // shutting down
			}
			slog.Error("accept failed", slog.Any("err", err))
			continue
		}
		go handleCommandConn(conn, cancel)
	}
}

func handleCommandConn(conn net.Conn, cancel context.CancelFunc) {
	defer conn.Close()

	var cmd Command
	if err := json.NewDecoder(conn).Decode(&cmd); err != nil {
		if !errors.Is(err, io.EOF) {
			slog.Error("invalid command received", slog.Any("err", err))
		}
		return
	}

	slog.Info("received command", slog.String("type", cmd.Type), slog.String("data", cmd.Data))

	switch cmd.Type {
	case "reload":
		slog.Info("reload command triggered (stub)")
	case "stop":
		slog.Info("stop command received, shutting down")
		cancel() // gracefully terminate
	default:
		slog.Warn("unknown command", slog.String("cmd", cmd.Type))
	}
}

// socket for managing -

func main() {
	opts := Opts{}

	// parse flags

	flag.StringVar(&opts.Directory, "D", "", "receive write-ahead log files into this directory")
	flag.StringVar(&opts.Directory, "directory", "", "(same as -D)")
	flag.StringVar(&opts.Slot, "S", "", "replication slot to use")
	flag.StringVar(&opts.Slot, "slot", "", "(same as -S)")
	flag.BoolVar(&opts.NoLoop, "n", false, "do not loop on connection lost")
	flag.BoolVar(&opts.NoLoop, "no-loop", false, "(same as -n)")
	flag.StringVar(&opts.LogLevel, "log-level", "info", "set log level (e.g., debug, info, warn, error)")
	flag.BoolVar(&opts.LogAddSource, "log-add-source", false, "include source file and line in log output")
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
	empty := []string{}
	for _, v := range requiredVars {
		if os.Getenv(v) == "" {
			empty = append(empty, v)
		}
	}
	if len(empty) > 0 {
		log.Fatalf("required vars are empty: [%s]", strings.Join(empty, " "))
	}

	connStrRepl := fmt.Sprintf("application_name=%s replication=yes", opts.Slot)

	// setup context

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Init()
	go startTCPListener(ctx, "127.0.0.1:5007", cancel)

	conn, err := pgconn.Connect(context.Background(), connStrRepl)
	if err != nil {
		slog.Error("cannot establish connection", slog.Any("err", err))
		//nolint:gocritic
		os.Exit(1)
	}
	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		log.Fatal(err)
	}
	pgrw := &xlog.PgReceiveWal{
		BaseDir:     opts.Directory,
		WalSegSz:    startupInfo.WalSegSz,
		Conn:        conn,
		ConnStrRepl: connStrRepl,
		SlotName:    opts.Slot,
	}
	pgrw.SetupSignalHandler()

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
