package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"pgreceivewal5/internal/logger"
	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pgx/v5/pgconn"
)

// TODO: CLI
const (
	slotName    = "pg_recval_5"
	connStrRepl = "application_name=pg_recval_5 user=postgres replication=yes host=localhost port=5432"
	baseDir     = "wals"
	noLoop      = false
)

func init() {
	// TODO:localdev
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_ADD_SOURCE", "1")
}

func main() {
	ctx := context.Background()
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

	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			os.Exit(1)
		}

		if noLoop {
			slog.Error("disconnected")
			os.Exit(1)
		}

		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
