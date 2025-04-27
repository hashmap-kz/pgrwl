package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"pgreceivewal5/internal/pg"

	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wal-g/tracelog"
)

const (
	slotName    = "pg_recval_5"
	connStr     = "postgresql://postgres:postgres@localhost:5432/postgres"
	connStrRepl = "application_name=pg_recval_5 user=postgres replication=yes"
)

var conn *pgconn.PgConn

type startupInfo struct {
	walSegSz uint64
}

func getStartupInfo() (*startupInfo, error) {
	qs, err := pg.NewPGQuerySession(context.TODO(), connStr)
	if err != nil {
		return nil, err
	}
	defer qs.Close()

	return &startupInfo{
		walSegSz: qs.WalSegmentSize(),
	}, nil
}

func StreamLog() error {
	var err error

	// 1
	startupInfo, err := getStartupInfo()
	tracelog.ErrorLogger.FatalOnError(err)
	walSegSz := startupInfo.walSegSz

	// 2
	if conn == nil {
		// 2
		conn, err = pgconn.Connect(context.Background(), connStrRepl)
		tracelog.ErrorLogger.FatalOnError(err)
		defer conn.Close(context.Background())
	}

	// 3
	var slotRestartInfo *xlog.ReadReplicationSlotResultResult
	_, err = xlog.GetSlotInformation(conn, slotName)
	if errors.Is(err, xlog.ErrSlotDoesNotExist) {
		tracelog.InfoLogger.Println("Trying to create the replication slot")
		_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slotName, "",
			pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
		tracelog.ErrorLogger.FatalOnError(err)
	} else {
		tracelog.ErrorLogger.FatalOnError(err)
	}
	slotRestartInfo, err = xlog.GetSlotInformation(conn, slotName)
	tracelog.ErrorLogger.FatalOnError(err)

	// 3
	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	tracelog.ErrorLogger.FatalOnError(err)

	// 4
	streamStartLSN, streamStartTimeline, err := xlog.FindStreamingStart("wals", walSegSz)
	if err != nil {
		if !errors.Is(err, xlog.ErrNoWalEntries) {
			tracelog.ErrorLogger.FatalOnError(err)
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

	stream := &xlog.StreamCtl{
		StartPos:              streamStartLSN,
		Timeline:              streamStartTimeline,
		StandbyMessageTimeout: 10 * time.Second,
		Synchronous:           true,
		PartialSuffix:         ".partial",
		StreamStop:            xlog.StopStreaming,
		ReplicationSlot:       slotName,
		SysIdentifier:         sysident.SystemID,
		WalSegSz:              walSegSz,
	}

	err = xlog.ReceiveXlogStream2(context.TODO(), conn, stream)
	return err
}

func main() {
	StreamLog()
}
