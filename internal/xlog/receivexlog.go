package xlog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

type StreamClient interface {
	StreamStop(blockpos pglogrepl.LSN, timeline uint32, endOfSegment bool) bool
}

// https://github.com/postgres/postgres/blob/master/src/bin/pg_basebackup/receivelog.c

type StreamCtl struct {
	StartPos              pglogrepl.LSN
	Timeline              uint32
	StandbyMessageTimeout time.Duration
	Synchronous           bool
	PartialSuffix         string
	StreamClient          StreamClient
	ReplicationSlot       string
	SysIdentifier         string
	WalSegSz              uint64

	LastFlushPosition   pglogrepl.LSN
	StillSending        bool
	ReportFlushPosition bool

	BaseDir string

	walfile *walfileT
}

func ProcessXLogDataMsg(
	conn *pgconn.PgConn,
	stream *StreamCtl,
	xld pglogrepl.XLogData,
	blockpos *pglogrepl.LSN,
) (bool, error) {
	/*
	 * Once we've decided we don't want to receive any more, just ignore any
	 * subsequent XLogData messages.
	 */
	if !stream.StillSending {
		return true, nil
	}

	xlogoff := XLogSegmentOffset(xld.WALStart, stream.WalSegSz)

	// If no open file, expect offset to be zero
	if stream.walfile == nil {
		if xlogoff != 0 {
			return false, fmt.Errorf("received WAL at offset %d but no file open", xlogoff)
		}
	} else {
		if stream.walfile.currpos != xlogoff {
			return false, fmt.Errorf("got WAL data offset %08x, expected %08x", xlogoff, stream.walfile.currpos)
		}
	}

	data := xld.WALData

	bytesLeft := uint64(len(data))
	bytesWritten := uint64(0)

	for bytesLeft != 0 {
		var bytesToWrite uint64

		/*
		 * If crossing a WAL boundary, only write up until we reach wal
		 * segment size.
		 */
		if xlogoff+bytesLeft > stream.WalSegSz {
			bytesToWrite = stream.WalSegSz - xlogoff
		} else {
			bytesToWrite = bytesLeft
		}

		if stream.walfile == nil {
			err := stream.OpenWalFile(*blockpos)
			if err != nil {
				return false, err
			}
		}

		// if (stream->walmethod->ops->write(walfile,
		// 								  copybuf + hdr_len + bytes_written,
		// 								  bytes_to_write) != bytes_to_write)
		// {
		// 	pg_log_error("could not write %d bytes to WAL file \"%s\": %s",
		// 				 bytes_to_write, walfile->pathname,
		// 				 GetLastWalMethodError(stream->walmethod));
		// 	return false;
		// }

		err := stream.WriteAtWalFile(data[bytesWritten:bytesWritten+bytesToWrite], int64(xlogoff))
		if err != nil {
			return false, fmt.Errorf("could not write %d bytes to WAL file: %w", bytesToWrite, err)
		}

		bytesWritten += bytesToWrite
		bytesLeft -= bytesToWrite
		*blockpos += pglogrepl.LSN(bytesToWrite)
		xlogoff += bytesToWrite

		xlSegOff := XLogSegmentOffset(*blockpos, stream.WalSegSz)
		if xlSegOff == 0 {
			err := stream.CloseWalfile(*blockpos)
			if err != nil {
				return false, err
			}
			xlogoff = 0

			if stream.StillSending && stream.StreamClient.StreamStop(pglogrepl.LSN(*blockpos), stream.Timeline, true) {
				// Send CopyDone message
				_, err := pglogrepl.SendStandbyCopyDone(context.Background(), conn)
				if err != nil {
					return false, fmt.Errorf("could not send copy-end packet: %w", err)
				}
				stream.StillSending = false
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
	if stream.StillSending && stream.StreamClient.StreamStop(blockpos, stream.Timeline, false) {
		// Close WAL file first
		if err := stream.CloseWalfile(blockpos); err != nil {
			// Error already logged in closeWalFile
			return false
		}

		// Send CopyDone
		_, err := pglogrepl.SendStandbyCopyDone(ctx, conn)
		if err != nil {
			log.Printf("could not send copy-end packet: %v", err)
			return false
		}

		stream.StillSending = false
	}

	return true
}

func sendFeedback(
	ctx context.Context,
	stream *StreamCtl,
	conn *pgconn.PgConn,
	blockpos pglogrepl.LSN,
	now time.Time,
	replyRequested bool,
) error {
	var standbyStatus pglogrepl.StandbyStatusUpdate

	standbyStatus.WALWritePosition = blockpos

	if stream.ReportFlushPosition {
		standbyStatus.WALFlushPosition = stream.LastFlushPosition
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

func HandleCopyStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl, stopPos *pglogrepl.LSN) error {
	var lastStatus time.Time
	blockPos := stream.StartPos

	stream.StillSending = true

	for {
		// Check if we should stop streaming
		if !checkCopyStreamStop(ctx, conn, stream, blockPos) {
			return errors.New("stream stop requested")
		}

		now := time.Now()

		// If synchronous, flush WAL file and update server immediately
		if stream.Synchronous && stream.LastFlushPosition < blockPos && stream.walfile != nil {
			if err := stream.SyncWalFile(); err != nil {
				return fmt.Errorf("could not fsync WAL file: %w", err)
			}
			stream.LastFlushPosition = blockPos
			if err := sendFeedback(ctx, stream, conn, blockPos, now, false); err != nil {
				return fmt.Errorf("could not send feedback after sync: %w", err)
			}
			lastStatus = now
		}

		// If time to send feedback
		if stream.StillSending && stream.StandbyMessageTimeout > 0 &&
			time.Since(lastStatus) > stream.StandbyMessageTimeout {
			if err := sendFeedback(ctx, stream, conn, blockPos, now, false); err != nil {
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
					if err := sendFeedback(ctx, stream, conn, blockPos, time.Now(), false); err != nil {
						return fmt.Errorf("send feedback on reply requested failed: %w", err)
					}
					lastStatus = time.Now()
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(m.Data[1:])
				if err != nil {
					return fmt.Errorf("parse xlogdata failed: %w", err)
				}

				if _, err := ProcessXLogDataMsg(conn, stream, xld, &blockPos); err != nil {
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
			slog.Info("HandleCopyStream: received CopyDone, HandleEndOfCopyStream()")

			// Server ended the stream. Handle CopyDone properly!
			// HandleEndOfCopyStream(PGconn *conn, StreamCtl *stream, char *copybuf, XLogRecPtr blockpos, XLogRecPtr *stoppos)
			if stream.StillSending {
				if err := stream.CloseWalfile(blockPos); err != nil {
					return fmt.Errorf("failed to close WAL file: %w", err)
				}
				if _, err := pglogrepl.SendStandbyCopyDone(ctx, conn); err != nil {
					return fmt.Errorf("failed to send client CopyDone: %w", err)
				}
				stream.StillSending = false
			}
			*stopPos = blockPos
			return nil

		default:
			return fmt.Errorf("unexpected backend message: %T", msg)
		}
	}
}

/////// v2

func ReceiveXlogStream2(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl) error {
	var stopPos pglogrepl.LSN

	stream.LastFlushPosition = stream.StartPos

	for {

		// Before starting, check if we need to fetch timeline history
		if !existsTimeLineHistoryFile(stream) {
			tlh, err := pglogrepl.TimelineHistory(ctx, conn, int32(stream.Timeline))
			if err != nil {
				return fmt.Errorf("failed to fetch timeline history: %w", err)
			}
			err = writeTimeLineHistoryFile(stream, tlh.FileName, string(tlh.Content))
			if err != nil {
				return fmt.Errorf("failed to write timeline history file: %w", err)
			}
		}

		// Check if we should stop before starting replication
		if stream.StreamClient.StreamStop(stream.StartPos, stream.Timeline, false) {
			return nil
		}

		// Start replication
		opts := pglogrepl.StartReplicationOptions{
			Timeline: int32(stream.Timeline),
			Mode:     pglogrepl.PhysicalReplication,
		}
		err := pglogrepl.StartReplication(ctx, conn, stream.ReplicationSlot, stream.StartPos, opts)
		if err != nil {
			return fmt.Errorf("failed to start replication: %w", err)
		}

		// Stream WAL
		err = HandleCopyStream(ctx, conn, stream, &stopPos)
		if err != nil {
			return fmt.Errorf("error during streaming: %w", err)
		}

		// After CopyDone:
		for {
			msg, err := conn.ReceiveMessage(ctx)
			if err != nil {
				return fmt.Errorf("failed receiving server result: %w", err)
			}

			switch m := msg.(type) {
			case *pgproto3.DataRow:
				// Server sends end-of-timeline info
				newTimeline, newStartPos, err := parseEndOfStreamingResult(m)
				if err != nil {
					return fmt.Errorf("could not parse end-of-stream result: %w", err)
				}
				if newTimeline <= stream.Timeline {
					return fmt.Errorf("server reported unexpected next timeline %d <= %d", newTimeline, stream.Timeline)
				}
				if newStartPos > stopPos {
					return fmt.Errorf("server reported next timeline startpos %s > stoppos %s", newStartPos, stopPos)
				}

				stream.Timeline = newTimeline
				stream.StartPos = newStartPos - (newStartPos % pglogrepl.LSN(stream.WalSegSz))
				goto nextIteration

			case *pgproto3.CommandComplete:
				if stream.StreamClient.StreamStop(stopPos, stream.Timeline, false) {
					return nil
				}
				return fmt.Errorf("replication stream terminated unexpectedly before stop point")

			case *pgproto3.ErrorResponse:
				return fmt.Errorf("error response from server: %s", m.Message)

			case *pgproto3.ReadyForQuery:
				// Ignore, ready for new commands
				continue

			default:
				// Unexpected
				continue
			}
		}
	nextIteration:
		continue
		// After CopyDone:

	}
}

func parseEndOfStreamingResult(m *pgproto3.DataRow) (newTimeline uint32, startLSN pglogrepl.LSN, err error) {
	if len(m.Values) != 2 {
		return 0, 0, fmt.Errorf("unexpected DataRow format")
	}
	timelineStr := string(m.Values[0])
	startLSNStr := string(m.Values[1])

	tli, err := strconv.ParseUint(timelineStr, 10, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid timeline value: %w", err)
	}
	lsn, err := pglogrepl.ParseLSN(startLSNStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid LSN value: %w", err)
	}
	return uint32(tli), lsn, nil
}

// timeline history file

func existsTimeLineHistoryFile(stream *StreamCtl) bool {
	// Timeline 1 never has a history file
	if stream.Timeline == 1 {
		return true
	}

	histfname := fmt.Sprintf("%08X.history", stream.Timeline)
	return fileExists(filepath.Join(stream.BaseDir, histfname))
}

// fileExists is a tiny helper for checking existence
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func writeTimeLineHistoryFile(stream *StreamCtl, filename, content string) error {
	expectedName := fmt.Sprintf("%08X.history", stream.Timeline)

	if expectedName != filename {
		return fmt.Errorf("server reported unexpected history file name for timeline %d: %s", stream.Timeline, filename)
	}

	histPath := filepath.Join(stream.BaseDir, filename+".tmp")

	// Create temporary file first
	f, err := os.OpenFile(histPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("could not create timeline history file %s: %w", histPath, err)
	}
	defer func() {
		_ = f.Close() // ensure close even on error
	}()

	// Write content
	n, err := f.WriteString(content)
	if err != nil || n != len(content) {
		_ = os.Remove(histPath) // delete temp file
		return fmt.Errorf("could not write timeline history file %s: %w", histPath, err)
	}

	// Sync to disk
	if err := f.Sync(); err != nil {
		return fmt.Errorf("could not fsync timeline history file %s: %w", histPath, err)
	}

	// Close file
	if err := f.Close(); err != nil {
		return fmt.Errorf("could not close timeline history file %s: %w", histPath, err)
	}

	// Rename from .tmp to final
	finalPath := filepath.Join(stream.BaseDir, filename)
	if err := os.Rename(histPath, finalPath); err != nil {
		return fmt.Errorf("could not rename temp timeline history file to final: %w", err)
	}

	return nil
}
