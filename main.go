package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

func init() {
	logLevel := os.Getenv("LOG_LEVEL")
	logFormat := os.Getenv("LOG_FORMAT")

	// Get logger level (INFO if not set)
	levels := map[string]slog.Level{
		"trace": slog.LevelDebug,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	lvl := slog.LevelInfo
	if cfgLvl, ok := levels[strings.ToLower(logLevel)]; ok {
		lvl = cfgLvl
	}

	// Create a base handler (TEXT if not set)
	var baseHandler slog.Handler
	if strings.ToLower(logFormat) == "json" {
		baseHandler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level: lvl,
		})
	} else {
		baseHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: lvl,
		})
	}

	// Add global "pid" attribute to all logs
	logger := slog.New(baseHandler.WithAttrs([]slog.Attr{
		slog.Int("pid", os.Getpid()),
	}))

	// Set it as the default logger for the project
	slog.SetDefault(logger)
}

// StreamLog the main loop of WAL receiving, any error FATAL
func StreamLog(ctx context.Context) error {
	var err error

	// 1
	if conn == nil {
		conn, err = pgconn.Connect(context.Background(), connStrRepl)
		if err != nil {
			slog.Error("cannot establish connection", slog.Any("err", err))
			// not a fatal error, a reconnect loop will handle it
			return nil
		}
	}

	// 2
	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		return fmt.Errorf("cannot get startup info (wal_segment_size, server_version_num): %w", err)
	}
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
			slog.Info("creating replication slot", slog.String("name", slotName))
			replicationSlotOptions := pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication}
			_, err = pglogrepl.CreateReplicationSlot(ctx, conn, slotName, "", replicationSlotOptions)
			if err != nil {
				return fmt.Errorf("cannot create replication slot: %w", err)
			}
		} else {
			return fmt.Errorf("cannot get slot information when checking existence: %w", err)
		}
	}

	slotRestartInfo, err = xlog.GetSlotInformation(conn, slotName)
	if err != nil {
		return fmt.Errorf("cannot get slot information: %w", err)
	}

	// 3
	sysident, err := pglogrepl.IdentifySystem(ctx, conn)
	if err != nil {
		return fmt.Errorf("cannot identify system: %w", err)
	}

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
		return fmt.Errorf("cannot find start LSN for streaming")
	}

	// 5

	// Always start streaming at the beginning of a segment
	curPos := uint64(streamStartLSN) - xlog.XLogSegmentOffset(streamStartLSN, walSegSz)
	streamStartLSN = pglogrepl.LSN(curPos)

	slog.Info("start streaming",
		slog.String("lsn", streamStartLSN.String()),
		slog.Uint64("tli", uint64(streamStartTimeline)),
	)

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

	err = xlog.ReceiveXlogStream3(ctx, conn, stream)
	if err != nil {
		slog.Error("stream terminated (ReceiveXlogStream3)", slog.Any("err", err))
	}

	// fsync dir
	err = xlog.FsyncDir(pgrw.BaseDir)
	if err != nil {
		slog.Info("could not finish writing WAL files", slog.Any("err", err))
		// not a fatal error, just log it
		return nil
	}

	if conn != nil {
		err := conn.Close(ctx)
		if err != nil {
			// not a fatal error, just log it
			slog.Info("could not close connection", slog.Any("err", err))
		}
		conn = nil
	}

	return nil
}

func main() {
	ctx := context.Background()

	for {
		err := StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting")
			os.Exit(1)
		}

		if noLoop {
			slog.Error("disconnected")
			os.Exit(1)
		} else {
			slog.Info("disconnected; waiting 5 seconds to try again")
			time.Sleep(5 * time.Second)
		}
	}
}
