package xlog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// https://github.com/postgres/postgres/blob/master/src/bin/pg_basebackup/receivelog.c

var (
	lastFlushPosition   pglogrepl.LSN
	stillSending        = true
	reportFlushPosition = true
)

type walfileT struct {
	currpos  int
	pathname string
	fd       *os.File
}

var walfile *walfileT

type StreamCtl struct {
	StartPos              pglogrepl.LSN
	Timeline              uint32
	StandbyMessageTimeout time.Duration
	Synchronous           bool
	PartialSuffix         string
	StreamStop            func(blockpos pglogrepl.LSN, timeline uint32, endOfSegment bool) bool
	ReplicationSlot       string
	SysIdentifier         string
}

func (w *walfileT) Sync() error {
	// TODO:fix
	return nil
}

func openWalFile(stream *StreamCtl, startpoint uint64) (*os.File, string, error) {
	segno := XLByteToSeg(startpoint, WalSegSz)
	filename := XLogFileName(stream.Timeline, segno, WalSegSz) + stream.PartialSuffix
	// TODO:fix
	fullPath := filepath.Join("wals", filename)

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		// File exists
		if stat.Size() == int64(WalSegSz) {
			// File already correctly sized, open it
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0o660)
			if err != nil {
				return nil, "", fmt.Errorf("could not open existing WAL file %s: %w", fullPath, err)
			}
			// Fsync to be safe
			if err := fd.Sync(); err != nil {
				fd.Close()
				return nil, "", fmt.Errorf("could not fsync WAL file %s: %w", fullPath, err)
			}
			return fd, fullPath, nil
		}
		if stat.Size() != 0 {
			return nil, "", fmt.Errorf("corrupt WAL file %s: expected size 0 or %d bytes, found %d", fullPath, WalSegSz, stat.Size())
		}
		// If size 0, proceed to initialize it
	}

	// Otherwise create new file and preallocate
	fd, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o660)
	if err != nil {
		return nil, "", fmt.Errorf("could not create WAL file %s: %w", fullPath, err)
	}

	// Preallocate file with zeros up to 16 MiB
	if err := fd.Truncate(int64(WalSegSz)); err != nil {
		fd.Close()
		return nil, "", fmt.Errorf("could not preallocate WAL file %s: %w", fullPath, err)
	}

	return fd, fullPath, nil
}

func closeWalfile(stream *StreamCtl, pos uint64) error {
	if walfile == nil {
		return nil
	}

	// TODO:fsync, simplify, etc...
	var err error
	if strings.HasSuffix(walfile.pathname, ".partial") {
		if uint64(walfile.currpos) == WalSegSz {
			err = closeAndRename()
		} else {
			err = closeNoRename()
		}
	} else {
		err = closeAndRename()
	}

	lastFlushPosition = pglogrepl.LSN(pos)
	return err
}

func closeNoRename() error {
	log.Printf("not renaming \"%s\", segment is not complete", walfile.pathname)
	err := walfile.fd.Close()
	if err != nil {
		return err
	}
	return nil
}

func closeAndRename() error {
	log.Printf("renaming \"%s\", segment is complete", walfile.pathname)
	err := walfile.fd.Close()
	if err != nil {
		return err
	}
	err = os.Rename(walfile.pathname, strings.TrimSuffix(walfile.pathname, ".partial"))
	if err != nil {
		return err
	}
	return nil
}

func ProcessXLogDataMsg(
	conn *pgconn.PgConn,
	stream *StreamCtl,
	xld pglogrepl.XLogData,
	blockpos *uint64,
) (bool, error) {
	/*
	 * Once we've decided we don't want to receive any more, just ignore any
	 * subsequent XLogData messages.
	 */
	if !stillSending {
		return true, nil
	}

	// if len(copybuf) < 1+8+8+8 {
	// 	return false, fmt.Errorf("streaming header too small: %d", len(copybuf))
	// }

	// xld, err := pglogrepl.ParseXLogData(copybuf[1:])
	// if err != nil {
	// 	return false, err
	// }

	xlogoff := XLogSegmentOffset(uint64(xld.WALStart), WalSegSz)

	// If no open file, expect offset to be zero
	if walfile == nil {
		if xlogoff != 0 {
			return false, fmt.Errorf("received WAL at offset %d but no file open", xlogoff)
		}
	} else {
		if walfile.currpos != xlogoff {
			return false, fmt.Errorf("got WAL data offset %08x, expected %08x", xlogoff, walfile.currpos)
		}
	}

	data := xld.WALData

	bytesLeft := len(data)
	bytesWritten := 0

	for bytesLeft > 0 {
		var bytesToWrite int

		/*
		 * If crossing a WAL boundary, only write up until we reach wal
		 * segment size.
		 */
		if uint64(xlogoff+bytesLeft) > WalSegSz {
			bytesToWrite = int(int(WalSegSz) - xlogoff)
		} else {
			bytesToWrite = bytesLeft
		}

		if walfile == nil {
			fd, fullpath, err := openWalFile(stream, *blockpos)
			if err != nil {
				return false, err
			}
			walfile = &walfileT{
				currpos:  int(*blockpos),
				pathname: fullpath,
				fd:       fd,
			}
		}

		_, err := walfile.fd.WriteAt(data[bytesWritten:bytesWritten+bytesToWrite], int64(xlogoff))
		if err != nil {
			return false, fmt.Errorf("could not write %d bytes to WAL file: %w", bytesToWrite, err)
		}

		bytesWritten += bytesToWrite
		bytesLeft -= bytesToWrite
		*blockpos += uint64(bytesToWrite)
		xlogoff += bytesToWrite

		if XLogSegmentOffset(*blockpos, WalSegSz) == 0 {
			err := closeWalfile(stream, *blockpos)
			if err != nil {
				return false, err
			}
			xlogoff = 0

			if stillSending && stream.StreamStop(pglogrepl.LSN(*blockpos), stream.Timeline, true) {
				// Send CopyDone message
				_, err := pglogrepl.SendStandbyCopyDone(context.Background(), conn)
				if err != nil {
					return false, fmt.Errorf("could not send copy-end packet: %w", err)
				}
				stillSending = false
				return true, nil
			}
		}

	}

	return true, nil
}

// handle copy //

func checkCopyStreamStop(
	ctx context.Context,
	conn *pgconn.PgConn,
	stream *StreamCtl,
	blockpos pglogrepl.LSN,
) bool {
	if stillSending && stream.StreamStop(blockpos, stream.Timeline, false) {
		// Close WAL file first
		if err := closeWalfile(stream, uint64(blockpos)); err != nil {
			// Error already logged in closeWalFile
			return false
		}

		// Send CopyDone
		_, err := pglogrepl.SendStandbyCopyDone(ctx, conn)
		if err != nil {
			log.Printf("could not send copy-end packet: %v", err)
			return false
		}

		stillSending = false
	}

	return true
}

func sendFeedback(
	ctx context.Context,
	conn *pgconn.PgConn,
	blockpos pglogrepl.LSN,
	now time.Time,
	replyRequested bool,
) error {
	var standbyStatus pglogrepl.StandbyStatusUpdate

	standbyStatus.WALWritePosition = blockpos

	if reportFlushPosition {
		standbyStatus.WALFlushPosition = lastFlushPosition
	} else {
		standbyStatus.WALFlushPosition = pglogrepl.LSN(0)
	}

	standbyStatus.WALApplyPosition = pglogrepl.LSN(0)
	standbyStatus.ClientTime = now
	standbyStatus.ReplyRequested = replyRequested

	err := pglogrepl.SendStandbyStatusUpdate(ctx, conn, standbyStatus)
	if err != nil {
		log.Printf("could not send feedback packet: %v", err)
		return err
	}

	return nil
}

func HandleCopyStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl) error {
	var lastStatus time.Time
	blockPos := stream.StartPos

	stillSending = true

	for {
		// Check if we should stop streaming
		if !checkCopyStreamStop(ctx, conn, stream, blockPos) {
			return errors.New("stream stop requested")
		}

		now := time.Now()

		// If synchronous, flush WAL file and update server immediately
		if stream.Synchronous && lastFlushPosition < blockPos && walfile != nil {
			if err := walfile.Sync(); err != nil {
				return fmt.Errorf("could not fsync WAL file: %w", err)
			}
			lastFlushPosition = blockPos
			if err := sendFeedback(ctx, conn, blockPos, now, false); err != nil {
				return fmt.Errorf("could not send feedback after sync: %w", err)
			}
			lastStatus = now
		}

		// If time to send feedback
		if stillSending && stream.StandbyMessageTimeout > 0 &&
			time.Since(lastStatus) > stream.StandbyMessageTimeout {
			if err := sendFeedback(ctx, conn, blockPos, now, false); err != nil {
				return fmt.Errorf("could not send periodic feedback: %w", err)
			}
			lastStatus = now
		}

		// Set next wake-up
		timeout := stream.StandbyMessageTimeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}

		ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
		msg, err := conn.ReceiveMessage(ctxTimeout)
		cancel()
		if pgconn.Timeout(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("receive message failed: %w", err)
		}

		switch m := msg.(type) {

		case *pgproto3.CopyData:
			switch m.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(m.Data[1:])
				if err != nil {
					return fmt.Errorf("parse keepalive failed: %w", err)
				}
				if pkm.ReplyRequested {
					if err := sendFeedback(ctx, conn, blockPos, time.Now(), false); err != nil {
						return fmt.Errorf("send feedback on reply requested failed: %w", err)
					}
					lastStatus = time.Now()
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(m.Data[1:])
				if err != nil {
					return fmt.Errorf("parse xlogdata failed: %w", err)
				}

				// TODO:types:fix
				bp := uint64(blockPos)
				if _, err := ProcessXLogDataMsg(conn, stream, xld, &bp); err != nil {
					return fmt.Errorf("processing xlogdata failed: %w", err)
				}

				// After processing XLogData, check again if stop is requested
				if !checkCopyStreamStop(ctx, conn, stream, blockPos) {
					return errors.New("stream stop requested after XLogData")
				}

			default:
				return fmt.Errorf("unexpected CopyData message type: %c", m.Data[0])
			}

		case *pgproto3.CopyDone:
			// Server ended streaming normally
			return nil

		default:
			return fmt.Errorf("unexpected backend message: %T", msg)
		}
	}
}

///////

func ReceiveXlogStream(conn *pgconn.PgConn, stream *StreamCtl) error {
	// var stoppos pglogrepl.LSN

	// TODO:fix:urgent
	//
	// // (Optional) Check PostgreSQL version for streaming compatibility
	// if !checkServerVersion(conn) {
	// 	return fmt.Errorf("server version incompatible")
	// }
	//
	// // Decide whether we should report flush position
	// if stream.ReplicationSlot != "" {
	// 	reportFlushPosition = true
	// } else {
	// 	reportFlushPosition = stream.Synchronous
	// }
	//
	// // Validate sysidentifier (optional)
	// if stream.SysIdentifier != "" {
	// 	sysident, servertli, err := RunIdentifySystem(context.Background(), conn)
	// 	if err != nil {
	// 		return fmt.Errorf("identify system failed: %w", err)
	// 	}
	// 	if stream.SysIdentifier != sysident {
	// 		return fmt.Errorf("system identifier mismatch between base backup and streaming connection")
	// 	}
	// 	if stream.Timeline > servertli {
	// 		return fmt.Errorf("starting timeline %d is not present on the server", stream.Timeline)
	// 	}
	// }

	lastFlushPosition = stream.StartPos

	for {
		// TODO:fix:urgent
		// // Check for timeline history fetch
		// if !existsTimeLineHistoryFile(stream) {
		// 	if err := fetchTimelineHistory(conn, stream); err != nil {
		// 		return fmt.Errorf("failed to fetch timeline history: %w", err)
		// 	}
		// }

		// Check if user wants to stop before streaming
		if stream.StreamStop(stream.StartPos, stream.Timeline, false) {
			return nil
		}

		// Start replication
		startOpts := pglogrepl.StartReplicationOptions{
			Timeline: int32(stream.Timeline),
			Mode:     pglogrepl.PhysicalReplication,
		}

		// TODO:fix:urgent
		// if stream.ReplicationSlot != "" {
		// 	startOpts.SlotName = stream.ReplicationSlot
		// }

		err := pglogrepl.StartReplication(context.Background(), conn, stream.ReplicationSlot, stream.StartPos, startOpts)
		if err != nil {
			return fmt.Errorf("start replication failed: %w", err)
		}

		// Stream the WAL
		err = HandleCopyStream(context.TODO(), conn, stream)
		if err != nil {
			return fmt.Errorf("streaming failed: %w", err)
		}

		// TODO:fix:urgent
		//
		// // After streaming: detect controlled shutdown vs timeline switch
		// nextTimeline, newStartPos, err := ReadEndOfStreamingResult(conn)
		// if err == nil {
		// 	// Server sent a new timeline
		// 	if nextTimeline <= stream.Timeline {
		// 		return fmt.Errorf("server reported unexpected next timeline %d after timeline %d", nextTimeline, stream.Timeline)
		// 	}
		// 	if newStartPos > stoppos {
		// 		return fmt.Errorf("server stopped timeline %d at %X/%X, but reported next timeline %d at %X/%X",
		// 			stream.Timeline, uint32(stoppos>>32), uint32(stoppos),
		// 			nextTimeline, uint32(newStartPos>>32), uint32(newStartPos))
		// 	}
		//
		// 	stream.Timeline = nextTimeline
		// 	stream.StartPos = newStartPos - pglogrepl.LSN(XLogSegmentOffset(newStartPos, WalSegSz))
		//
		// 	continue // Restart streaming on next timeline
		// } else if errors.Is(err, errControlledShutdown) {
		// 	// Server shutdown normally
		// 	if stream.StreamStop(stoppos, stream.Timeline, false) {
		// 		return nil
		// 	}
		// 	return fmt.Errorf("replication stream was terminated before stop point")
		// } else {
		// 	return fmt.Errorf("unexpected termination of replication stream: %w", err)
		// }
	}
}
