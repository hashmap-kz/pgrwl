package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	slotName    = "pg_recval_5"
	connStrRepl = "application_name=pg_recval_5 user=postgres replication=yes"
	baseDir     = "wals"
	noLoop      = false
)

var conn *pgconn.PgConn

func FatalOnErr(err error, msg string, args ...any) {
	if err != nil {
		slog.Error(msg, append(args, "error", err)...)
		os.Exit(1)
	}
}

func init() {
	lvlCfg := os.Getenv("LOG_LEVEL")
	// Init logger
	levels := map[string]slog.Level{
		"trace": slog.LevelDebug,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	lvl := slog.LevelInfo
	if cfgLvl, ok := levels[lvlCfg]; ok {
		lvl = cfgLvl
	}
	// Create a base handler
	baseHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: lvl,
	})
	// Add global "app" attribute to all logs
	logger := slog.New(baseHandler.WithAttrs([]slog.Attr{
		slog.String("app", "x05"),
	}))
	// Set it as the default logger for the project
	slog.SetDefault(logger)
}

func StreamLog() {
	var err error

	// 1
	if conn == nil {
		conn, err = pgconn.Connect(context.Background(), connStrRepl)
		if err != nil {
			slog.Error("cannot establish connection", slog.Any("err", err))
			return
		}
	}

	// 2
	startupInfo, err := xlog.GetStartupInfo(conn)
	FatalOnErr(err, "cannot get startup info (wal_segment_size, server_version_num)")
	walSegSz := startupInfo.WalSegSz

	pgrw := &xlog.PgReceiveWal{
		Verbose:  true,
		BaseDir:  baseDir,
		WalSegSz: walSegSz,
	}

	// 3
	var slotRestartInfo *xlog.ReadReplicationSlotResultResult
	_, err = xlog.GetSlotInformation(conn, slotName)
	if err != nil {
		if errors.Is(err, xlog.ErrSlotDoesNotExist) {
			slog.Info("creating replication slot")
			_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slotName, "",
				pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
			FatalOnErr(err, "cannot create replication slot")
		} else {
			FatalOnErr(err, "cannot get slot information")
		}
	}

	slotRestartInfo, err = xlog.GetSlotInformation(conn, slotName)
	FatalOnErr(err, "cannot get slot information")

	// 3
	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	FatalOnErr(err, "cannot identify system")

	// 4
	streamStartLSN, streamStartTimeline, err := pgrw.FindStreamingStart()
	if err != nil {
		if !errors.Is(err, xlog.ErrNoWalEntries) {
			// just log an error and continue, stream-start-lsn and timeline
			// are required, and we will proceed with slot-info or sysident
			slog.Error("cannot find streaming start", slog.Any("err", err))
		}
	}

	if streamStartLSN == 0 {
		if slotRestartInfo.RestartLSN != 0 {
			streamStartLSN = slotRestartInfo.RestartLSN
			streamStartTimeline = slotRestartInfo.RestartTLI
		}
	}

	if streamStartLSN == 0 {
		streamStartLSN = sysident.XLogPos
		streamStartTimeline = uint32(sysident.Timeline)
	}

	// final check
	if streamStartLSN == 0 || streamStartTimeline == 0 {
		err := fmt.Errorf("cannot find start LSN for streaming")
		FatalOnErr(err, "")
	}

	// 5

	// Always start streaming at the beginning of a segment
	curPos := uint64(streamStartLSN) - xlog.XLogSegmentOffset(streamStartLSN, walSegSz)
	streamStartLSN = pglogrepl.LSN(curPos)

	stream := &xlog.StreamCtl{
		StartPos:              streamStartLSN,
		Timeline:              streamStartTimeline,
		StandbyMessageTimeout: 10 * time.Second,
		Synchronous:           true,
		PartialSuffix:         ".partial",
		StreamClient:          pgrw,
		ReplicationSlot:       slotName,
		SysIdentifier:         sysident.SystemID,
		WalSegSz:              walSegSz,

		LastFlushPosition:   pglogrepl.LSN(0),
		StillSending:        true,
		ReportFlushPosition: true,

		BaseDir: baseDir,
	}

	err = xlog.ReceiveXlogStream2(context.TODO(), conn, stream)
	if err != nil {
		slog.Error("stream terminated (ReceiveXlogStream2)", slog.Any("err", err))
	}

	// fsync dir
	err = xlog.FsyncFname(pgrw.BaseDir)
	if err != nil {
		slog.Info("could not finish writing WAL files", slog.Any("err", err))
		return
	}

	if conn != nil {
		conn.Close(context.Background())
		conn = nil
	}
}

func main() {
	for {
		StreamLog()

		if noLoop {
			slog.Error("disconnected")
			os.Exit(1)
		} else {
			slog.Info("disconnected; waiting 5 seconds to try again")
			time.Sleep(5 * time.Second)
		}
	}
}
