package main

import (
	"context"
	"log"
	"log/slog"
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
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_ADD_SOURCE", "1")

	os.Setenv("PGHOST", "localhost")
	os.Setenv("PGPORT", "5432")
	os.Setenv("PGUSER", "postgres")
	os.Setenv("PGPASSWORD", "postgres")
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	logger.Init()

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
