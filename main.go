package main

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	slotInfo *pg.PhysicalSlot
	walSegSz uint64
}

func getStartupInfo() (*startupInfo, error) {
	qs, err := pg.NewPGQuerySession(context.TODO(), connStr)
	if err != nil {
		return nil, err
	}
	defer qs.Close()

	slotInfo, err := qs.SlotInfo(slotName)
	if err != nil {
		return nil, err
	}

	return &startupInfo{
		slotInfo: &slotInfo,
		walSegSz: qs.WalSegmentSize(),
	}, nil
}

// /*
//   - Start the log streaming
//     */
//
// static void
// StreamLog(void)
//
//	{
//		XLogRecPtr	serverpos;
//		TimeLineID	servertli;
//		StreamCtl	stream = {0};
//		char	   *sysidentifier;
//
//		/*
//		 * Connect in replication mode to the server
//		 */
//		if (conn == NULL)
//			conn = GetConnection();
//		if (!conn)
//			/* Error message already written in GetConnection() */
//			return;
//
//		if (!CheckServerVersionForStreaming(conn))
//		{
//			/*
//			 * Error message already written in CheckServerVersionForStreaming().
//			 * There's no hope of recovering from a version mismatch, so don't
//			 * retry.
//			 */
//			exit(1);
//		}
//
//		/*
//		 * Identify server, obtaining start LSN position and current timeline ID
//		 * at the same time, necessary if not valid data can be found in the
//		 * existing output directory.
//		 */
//		if (!RunIdentifySystem(conn, &sysidentifier, &servertli, &serverpos, NULL))
//			exit(1);
//
//		/*
//		 * Figure out where to start streaming.  First scan the local directory.
//		 */
//		stream.startpos = FindStreamingStart(&stream.timeline);
//		if (stream.startpos == InvalidXLogRecPtr)
//		{
//			/*
//			 * Try to get the starting point from the slot if any.  This is
//			 * supported in PostgreSQL 15 and newer.
//			 */
//			if (replication_slot != NULL &&
//				PQserverVersion(conn) >= 150000)
//			{
//				if (!GetSlotInformation(conn, replication_slot, &stream.startpos,
//										&stream.timeline))
//				{
//					/* Error is logged by GetSlotInformation() */
//					return;
//				}
//			}
//
//			/*
//			 * If it the starting point is still not known, use the current WAL
//			 * flush value as last resort.
//			 */
//			if (stream.startpos == InvalidXLogRecPtr)
//			{
//				stream.startpos = serverpos;
//				stream.timeline = servertli;
//			}
//		}
//
//		Assert(stream.startpos != InvalidXLogRecPtr &&
//			   stream.timeline != 0);
//
//		/*
//		 * Always start streaming at the beginning of a segment
//		 */
//		stream.startpos -= XLogSegmentOffset(stream.startpos, WalSegSz);
//
//		/*
//		 * Start the replication
//		 */
//		if (verbose)
//			pg_log_info("starting log streaming at %X/%X (timeline %u)",
//						LSN_FORMAT_ARGS(stream.startpos),
//						stream.timeline);
//
//		stream.stream_stop = stop_streaming;
//		stream.stop_socket = PGINVALID_SOCKET;
//		stream.standby_message_timeout = standby_message_timeout;
//		stream.synchronous = synchronous;
//		stream.do_sync = do_sync;
//		stream.mark_done = false;
//		stream.walmethod = CreateWalDirectoryMethod(basedir,
//													compression_algorithm,
//													compresslevel,
//													stream.do_sync);
//		stream.partial_suffix = ".partial";
//		stream.replication_slot = replication_slot;
//		stream.sysidentifier = sysidentifier;
//
//		ReceiveXlogStream(conn, &stream);
//
//		if (!stream.walmethod->ops->finish(stream.walmethod))
//		{
//			pg_log_info("could not finish writing WAL files: %m");
//			return;
//		}
//
//		PQfinish(conn);
//		conn = NULL;
//
//		stream.walmethod->ops->free(stream.walmethod);
//	}
func StreamLog() error {
	// 1
	startupInfo, err := getStartupInfo()
	tracelog.ErrorLogger.FatalOnError(err)
	slotInfo := startupInfo.slotInfo
	walSegSz := startupInfo.walSegSz

	// 2
	if conn == nil {
		// 2
		conn, err = pgconn.Connect(context.Background(), connStrRepl)
		tracelog.ErrorLogger.FatalOnError(err)
		defer conn.Close(context.Background())
	}

	// 3
	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	tracelog.ErrorLogger.FatalOnError(err)

	if !slotInfo.Exists {
		tracelog.InfoLogger.Println("Trying to create the replication slot")
		_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slotInfo.Name, "",
			pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
		tracelog.ErrorLogger.FatalOnError(err)
	}

	// 4
	streamStartLSN, streamStartTimeline, err := xlog.FindStreamingStart("wals", walSegSz)
	if err != nil {
		if !errors.Is(err, xlog.ErrNoWalEntries) {
			tracelog.ErrorLogger.FatalOnError(err)
		}
	}

	if streamStartLSN == 0 {
		if slotInfo.RestartLSN != 0 {
			streamStartLSN = slotInfo.RestartLSN
		}
	}

	fmt.Println(sysident, streamStartTimeline)

	return nil
}

func main() {
	// 1
	startupInfo, err := getStartupInfo()
	tracelog.ErrorLogger.FatalOnError(err)
	slotInfo := startupInfo.slotInfo
	walSegSz := startupInfo.walSegSz

	// 2
	conn, err := pgconn.Connect(context.Background(), connStrRepl)
	tracelog.ErrorLogger.FatalOnError(err)
	defer conn.Close(context.Background())

	sysident, err := pglogrepl.IdentifySystem(context.Background(), conn)
	tracelog.ErrorLogger.FatalOnError(err)

	if !slotInfo.Exists {
		tracelog.InfoLogger.Println("Trying to create the replication slot")
		_, err = pglogrepl.CreateReplicationSlot(context.Background(), conn, slotInfo.Name, "",
			pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication})
		tracelog.ErrorLogger.FatalOnError(err)
	}

	// 3

	streamStartLSN, streamStartTimeline, _ := xlog.FindStreamingStart("wals", walSegSz)
	// TODO:fix:urgent
	// if err != nil {
	// 	log.Fatal(err)
	// }
	if streamStartLSN == 0 {
		streamStartLSN = sysident.XLogPos
		streamStartTimeline = uint32(sysident.Timeline)
	}

	stream := &xlog.StreamCtl{
		StartPos:              streamStartLSN,
		Timeline:              streamStartTimeline,
		StandbyMessageTimeout: 10 * time.Second,
		Synchronous:           true,
		PartialSuffix:         ".partial",
		StreamStop:            xlog.StopStreaming,
		ReplicationSlot:       "pg_recval_5",
		SysIdentifier:         sysident.SystemID,
		WalSegSz:              walSegSz,
	}

	curPos := uint64(stream.StartPos) - xlog.XLogSegmentOffset(stream.StartPos, walSegSz)
	stream.StartPos = pglogrepl.LSN(curPos)

	err = xlog.ReceiveXlogStream2(context.TODO(), conn, stream)
	if err != nil {
		log.Fatal(err)
	}

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
