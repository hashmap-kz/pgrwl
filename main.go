package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"pgreceivewal5/internal/postgres"
	"pgreceivewal5/internal/xlog"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wal-g/tracelog"
)

func getCurrentWalInfo() (slot postgres.PhysicalSlot, walSegmentBytes uint64, err error) {
	slotName := "pg_recval_5"

	// Creating a temporary connection to read slot info and wal_segment_size
	tmpConn, err := postgres.Connect()
	if err != nil {
		return
	}
	defer tmpConn.Close(context.TODO())

	queryRunner, err := postgres.NewPgQueryRunner(tmpConn)
	if err != nil {
		return
	}

	slot, err = queryRunner.GetPhysicalSlotInfo(slotName)
	if err != nil {
		return
	}

	walSegmentBytes, err = queryRunner.GetWalSegmentBytes()
	return
}

func main() {
	// 1
	slot, ws, err := getCurrentWalInfo()
	if err != nil {
		log.Fatal(err)
	}
	// TODO: check range
	xlog.WalSegSz = ws

	// 2
	conn, err := pgconn.Connect(context.Background(), "application_name=walg_test_slot user=postgres replication=yes")
	tracelog.ErrorLogger.FatalOnError(err)
	defer conn.Close(context.Background())

	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	tracelog.ErrorLogger.FatalOnError(err)

	if !slot.Exists {
		tracelog.InfoLogger.Println("Trying to create the replication slot")
		_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slot.Name, "",
			pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
		tracelog.ErrorLogger.FatalOnError(err)
	}

	// 3

	streamStartLSN, streamStartTimeline, err := xlog.FindStreamingStart("wals")
	if err != nil {
		log.Fatal(err)
	}
	if streamStartLSN == 0 {
		streamStartLSN = sysident.XLogPos
	}

	stream := &xlog.StreamCtl{
		StartPos:              streamStartLSN,
		Timeline:              streamStartTimeline,
		StandbyMessageTimeout: 10 * time.Second,
		Synchronous:           true,
		PartialSuffix:         ".partial",
		StreamStop:            xlog.StopStreaming,
	}

	fmt.Println(stream)

	//	/*
	//	 * Figure out where to start streaming.  First scan the local directory.
	//	 */
	//	stream.startpos = FindStreamingStart(&stream.timeline);
	//	if (stream.startpos == InvalidXLogRecPtr)
	//	{
	//		/*
	//		 * Try to get the starting point from the slot if any.  This is
	//		 * supported in PostgreSQL 15 and newer.
	//		 */
	//		if (replication_slot != NULL &&
	//			PQserverVersion(conn) >= 150000)
	//		{
	//			if (!GetSlotInformation(conn, replication_slot, &stream.startpos,
	//									&stream.timeline))
	//			{
	//				/* Error is logged by GetSlotInformation() */
	//				return;
	//			}
	//		}
	//
	//		/*
	//		 * If it the starting point is still not known, use the current WAL
	//		 * flush value as last resort.
	//		 */
	//		if (stream.startpos == InvalidXLogRecPtr)
	//		{
	//			stream.startpos = serverpos;
	//			stream.timeline = servertli;
	//		}
	//	}
	//
	//	Assert(stream.startpos != InvalidXLogRecPtr &&
	//		   stream.timeline != 0);
	//
	//	/*
	//	 * Always start streaming at the beginning of a segment
	//	 */
	//	stream.startpos -= XLogSegmentOffset(stream.startpos, WalSegSz);
	//
	//	/*
	//	 * Start the replication
	//	 */
	//	if (verbose)
	//		pg_log_info("starting log streaming at %X/%X (timeline %u)",
	//					LSN_FORMAT_ARGS(stream.startpos),
	//					stream.timeline);
	//
	//	stream.stream_stop = stop_streaming;
	//	stream.stop_socket = PGINVALID_SOCKET;
	//	stream.standby_message_timeout = standby_message_timeout;
	//	stream.synchronous = synchronous;
	//	stream.do_sync = do_sync;
	//	stream.mark_done = false;
	//	stream.walmethod = CreateWalDirectoryMethod(basedir,
	//												compression_algorithm,
	//												compresslevel,
	//												stream.do_sync);
	//	stream.partial_suffix = ".partial";
	//	stream.replication_slot = replication_slot;
	//	stream.sysidentifier = sysidentifier;
	//
	//	ReceiveXlogStream(conn, &stream);
}
