package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pgreceivewal5/internal/logger"
	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pgx/v5/pgconn"
)

// TODO: CLI
const (
	slotName    = "pg_recval_5"
	connStrRepl = "application_name=pg_recval_5 replication=yes"
	baseDir     = "wals"
	noLoop      = false
)

func init() {
	// TODO:localdev
	os.Setenv("LOG_LEVEL", "info")
	os.Setenv("LOG_ADD_SOURCE", "1")

	os.Setenv("PGHOST", "localhost")
	os.Setenv("PGPORT", "5432")
	os.Setenv("PGUSER", "postgres")
	os.Setenv("PGPASSWORD", "postgres")
}

// socket for managing

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

// socket for managing

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Init()
	go startTCPListener(ctx, "127.0.0.1:5007", cancel)

	conn, err := pgconn.Connect(context.Background(), connStrRepl)
	if err != nil {
		slog.Error("cannot establish connection", slog.Any("err", err))
		os.Exit(1)
	}
	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		log.Fatal(err)
	}
	pgrw := &xlog.PgReceiveWal{
		BaseDir:     baseDir,
		WalSegSz:    startupInfo.WalSegSz,
		Conn:        conn,
		ConnStrRepl: connStrRepl,
		SlotName:    slotName,
	}
	// TODO:fix
	pgrw.SetupSignalHandler()

	//// compare streaming with pg_receivewal
	//logfile, err := os.Create("cmplogs.log")
	//if err != nil {
	//	log.Fatal(err)
	//}
	//defer logfile.Close()
	//go func() {
	//	for {
	//		diffs, err := testutils.CompareDirs("./wals", "./hack/pg_receivewal/wals")
	//		if err != nil {
	//			slog.Error("dircmp", slog.Any("err", err))
	//		}
	//		if len(diffs) != 0 {
	//			_, _ = logfile.WriteString(fmt.Sprintf("diffs: %+v\n", diffs))
	//		}
	//		time.Sleep(5 * time.Second)
	//	}
	//}()
	//// compare streaming with pg_receivewal

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

		if noLoop {
			slog.Error("disconnected")
			os.Exit(1)
		}

		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
