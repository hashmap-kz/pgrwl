package xlog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"pgreceivewal5/internal/fsync"

	"pgreceivewal5/internal/conv"

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
) error {
	var err error

	/*
	 * Once we've decided we don't want to receive any more, just ignore any
	 * subsequent XLogData messages.
	 */
	if !stream.StillSending {
		return nil
	}

	*blockpos = xld.WALStart

	xlogoff := XLogSegmentOffset(xld.WALStart, stream.WalSegSz)

	// If no open file, expect offset to be zero
	if stream.walfile == nil {
		if xlogoff != 0 {
			return fmt.Errorf("received WAL at offset %d but no file open", xlogoff)
		}
	} else {
		if stream.walfile.currpos != xlogoff {
			return fmt.Errorf("got WAL data offset %08x, expected %08x", xlogoff, stream.walfile.currpos)
		}
	}

	data := xld.WALData

	bytesLeft := uint64(len(data))
	bytesWritten := uint64(0)

	slog.Debug(fmt.Sprintf("received xlog-data, len %d lastFlushPosition %d blockpos %d diff %d",
		bytesLeft,
		stream.LastFlushPosition,
		*blockpos,
		*blockpos-stream.LastFlushPosition,
	))

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
			err = stream.OpenWalFile(*blockpos)
			if err != nil {
				return err
			}
		}

		// if(stream->walmethod->ops->write(walfile,
		// 	copybuf + hdr_len + bytes_written,
		// 	bytes_to_write) != bytes_to_write) {}

		n, err := stream.WriteAtWalFile(data[bytesWritten:bytesWritten+bytesToWrite], xlogoff)
		if err != nil {
			return fmt.Errorf("could not write %d bytes to WAL file: %w", bytesToWrite, err)
		}
		if conv.ToUint64(int64(n)) != bytesToWrite {
			return fmt.Errorf("could not write %d bytes to WAL file: %w", bytesToWrite, err)
		}

		bytesWritten += bytesToWrite
		bytesLeft -= bytesToWrite
		*blockpos += pglogrepl.LSN(bytesToWrite)
		xlogoff += bytesToWrite

		xlSegOff := XLogSegmentOffset(*blockpos, stream.WalSegSz)
		if xlSegOff == 0 {
			err := stream.CloseWalfile(*blockpos)
			if err != nil {
				return err
			}
			xlogoff = 0

			if stream.StillSending && stream.StreamClient.StreamStop(*blockpos, stream.Timeline, true) {
				// Send CopyDone message
				_, err := SendStandbyCopyDone(context.Background(), conn)
				if err != nil {
					return fmt.Errorf("could not send copy-end packet: %w", err)
				}
				stream.StillSending = false
				return nil /* ignore the rest of this XLogData packet */
			}
		}
	}
	return nil
}

// handle copy //

/*
 * Check if we should continue streaming, or abort at this point.
 */
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
		_, err := SendStandbyCopyDone(ctx, conn)
		if err != nil {
			slog.Error("could not send copy-end packet", slog.Any("err", err))
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
		slog.Error("could not send feedback packet", slog.Any("err", err))
		return err
	}

	return nil
}

func ProcessKeepaliveMsg(ctx context.Context,
	conn *pgconn.PgConn,
	stream *StreamCtl,
	keepalive pglogrepl.PrimaryKeepaliveMessage,
	blockPos pglogrepl.LSN,
	lastStatus *time.Time,
) error {
	slog.Info("ProcessKeepaliveMsg")

	if keepalive.ReplyRequested && stream.StillSending {
		// If a valid flush location needs to be reported, and WAL file exists
		if stream.ReportFlushPosition &&
			stream.LastFlushPosition < blockPos &&
			stream.walfile != nil {
			/*
			 * If a valid flush location needs to be reported, flush the
			 * current WAL file so that the latest flush location is sent back
			 * to the server. This is necessary to see whether the last WAL
			 * data has been successfully replicated or not, at the normal
			 * shutdown of the server.
			 */

			slog.Debug("SYNC-K",
				slog.Uint64("lastFlushPosition", uint64(stream.LastFlushPosition)),
				slog.Uint64("blockPos", uint64(blockPos)),
				slog.Uint64("diff", uint64(blockPos)-uint64(stream.LastFlushPosition)),
			)

			if err := stream.SyncWalFile(); err != nil {
				return fmt.Errorf("could not fsync WAL file: %w", err)
			}
			stream.LastFlushPosition = blockPos
		}

		now := time.Now()
		if err := sendFeedback(ctx, stream, conn, blockPos, now, false); err != nil {
			return fmt.Errorf("failed to send feedback in keepalive: %w", err)
		}
		*lastStatus = now
	}

	return nil
}

func HandleCopyStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl, stopPos *pglogrepl.LSN) (*pglogrepl.CopyDoneResult, error) {
	var lastStatus time.Time
	blockPos := stream.StartPos

	stream.StillSending = true

	for {

		/*
		 * Check if we should continue streaming, or abort at this point.
		 */
		if !checkCopyStreamStop(ctx, conn, stream, blockPos) {
			return nil, fmt.Errorf("check copy stream stop")
		}

		now := time.Now()

		// If synchronous, flush WAL file and update server immediately
		if stream.Synchronous && stream.LastFlushPosition < blockPos && stream.walfile != nil {
			slog.Debug("SYNC-1",
				slog.Uint64("lastFlushPosition", uint64(stream.LastFlushPosition)),
				slog.Uint64("blockPos", uint64(blockPos)),
				slog.Uint64("diff", uint64(blockPos)-uint64(stream.LastFlushPosition)),
			)

			if err := stream.SyncWalFile(); err != nil {
				return nil, fmt.Errorf("could not fsync WAL file: %w", err)
			}
			stream.LastFlushPosition = blockPos
			/*
			 * Send feedback so that the server sees the latest WAL locations
			 * immediately.
			 */
			if err := sendFeedback(ctx, stream, conn, blockPos, now, false); err != nil {
				return nil, fmt.Errorf("could not send feedback after sync: %w", err)
			}
			lastStatus = now
		}

		/*
		 * Potentially send a status message to the primary
		 */
		if stream.StillSending && stream.StandbyMessageTimeout > 0 &&
			time.Since(lastStatus) > stream.StandbyMessageTimeout {
			/* Time to send feedback! */
			if err := sendFeedback(ctx, stream, conn, blockPos, now, false); err != nil {
				return nil, fmt.Errorf("could not send periodic feedback: %w", err)
			}
			lastStatus = now
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			/*
			 * Calculate how long send/receive loops should sleep
			 */
			sleeptime := calculateCopyStreamSleepTime(stream, now, stream.StandbyMessageTimeout, lastStatus)

			ctxTimeout, cancel := context.WithTimeout(ctx, sleeptime)
			msg, err := conn.ReceiveMessage(ctxTimeout)
			cancel()
			if pgconn.Timeout(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("receive message failed: %w", err)
			}

			switch m := msg.(type) {
			case *pgproto3.CopyData:
				switch m.Data[0] {
				case pglogrepl.PrimaryKeepaliveMessageByteID:
					pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(m.Data[1:])
					if err != nil {
						return nil, fmt.Errorf("parse keepalive failed: %w", err)
					}
					if err := ProcessKeepaliveMsg(ctx, conn, stream, pkm, blockPos, &lastStatus); err != nil {
						return nil, fmt.Errorf("process keepalive failed: %w", err)
					}

				case pglogrepl.XLogDataByteID:
					xld, err := pglogrepl.ParseXLogData(m.Data[1:])
					if err != nil {
						return nil, fmt.Errorf("parse xlogdata failed: %w", err)
					}

					slog.Debug("X - ProcessXLogDataMsg START")
					if err := ProcessXLogDataMsg(conn, stream, xld, &blockPos); err != nil {
						return nil, fmt.Errorf("processing xlogdata failed: %w", err)
					}
					slog.Debug("X - ProcessXLogDataMsg END")

					/*
					 * Check if we should continue streaming, or abort at this
					 * point.
					 */
					if !checkCopyStreamStop(ctx, conn, stream, blockPos) {
						return nil, errors.New("stream stop requested after XLogData")
					}

				default:
					return nil, fmt.Errorf("unexpected CopyData message type: %c", m.Data[0])
				}

			case *pgproto3.CopyDone:
				return handleEndOfCopyStream(ctx, conn, stream, blockPos, stopPos)

				// Handle other commands
			case *pgproto3.CommandComplete:
				slog.Warn("received CommandComplete, treating as disconnection")
				return nil, nil // safe exit
			case *pgproto3.ErrorResponse:
				return nil, fmt.Errorf("error response from server: %s", m.Message)
			case *pgproto3.ReadyForQuery:
				slog.Warn("received ReadyForQuery, treating as disconnection")
				return nil, nil // safe exit
			default:
				slog.Warn("received unexpected message", slog.String("type", fmt.Sprintf("%T", msg)))
				return nil, fmt.Errorf("received unexpected message: %T", msg)
			}
		}
	}
}

func handleEndOfCopyStream(
	ctx context.Context,
	conn *pgconn.PgConn,
	stream *StreamCtl,
	blockPos pglogrepl.LSN,
	stopPos *pglogrepl.LSN,
) (*pglogrepl.CopyDoneResult, error) {
	slog.Info("HandleCopyStream: received CopyDone, HandleEndOfCopyStream()")
	var err error
	var cdr *pglogrepl.CopyDoneResult

	if stream.StillSending {
		if err := stream.CloseWalfile(blockPos); err != nil {
			return nil, fmt.Errorf("failed to close WAL file: %w", err)
		}

		cdr, err = SendStandbyCopyDone(ctx, conn)
		if err != nil {
			return nil, fmt.Errorf("failed to send client CopyDone: %w", err)
		}
		slog.Debug("copy-done-result", slog.Any("cdr", cdr))
		stream.StillSending = false
	}
	*stopPos = blockPos
	return cdr, nil
}

/////// v3

func ReceiveXlogStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl) error {
	var stopPos pglogrepl.LSN

	stream.LastFlushPosition = stream.StartPos

	for {
		// --- Before streaming starts ---

		timelineToI32, err := conv.Uint32ToInt32(stream.Timeline)
		if err != nil {
			return err
		}

		// Before starting, check if we need to fetch timeline history
		if !existsTimeLineHistoryFile(stream) {
			tlh, err := pglogrepl.TimelineHistory(ctx, conn, timelineToI32)
			if err != nil {
				return fmt.Errorf("failed to fetch timeline history: %w", err)
			}
			if err := writeTimeLineHistoryFile(stream, tlh.FileName, string(tlh.Content)); err != nil {
				return fmt.Errorf("failed to write timeline history file: %w", err)
			}
		}

		// Check if we should stop before starting replication
		if stream.StreamClient.StreamStop(stream.StartPos, stream.Timeline, false) {
			return nil
		}

		// Start replication
		opts := pglogrepl.StartReplicationOptions{
			Timeline: timelineToI32,
			Mode:     pglogrepl.PhysicalReplication,
		}
		if err := pglogrepl.StartReplication(ctx, conn, stream.ReplicationSlot, stream.StartPos, opts); err != nil {
			return fmt.Errorf("failed to start replication: %w", err)
		}

		// Stream WAL
		cdr, err := HandleCopyStream(ctx, conn, stream, &stopPos)
		if err != nil {
			closeWalfileNoRename(stream, "func HandleCopyStream() exits with an error")
			return fmt.Errorf("error during streaming: %w", err)
		}

		// TODO: revise
		// --- After CopyDone ---

		if cdr != nil {
			// Server sends end-of-timeline info
			newTimeline := conv.ToUint32(cdr.Timeline)
			newStartPos := cdr.LSN

			if newTimeline <= stream.Timeline {
				closeWalfileNoRename(stream, "newTimeline <= stream.Timeline")
				return fmt.Errorf("server reported unexpected next timeline %d <= %d", newTimeline, stream.Timeline)
			}
			if newStartPos > stopPos {
				closeWalfileNoRename(stream, "newStartPos > stopPos")
				return fmt.Errorf("server reported next timeline startpos %s > stoppos %s", newStartPos, stopPos)
			}

			/*
			* Loop back to start streaming from the new timeline. Always
			* start streaming at the beginning of a segment.
			 */
			stream.Timeline = newTimeline
			stream.StartPos = newStartPos - (newStartPos % pglogrepl.LSN(stream.WalSegSz))

			continue // restart streaming
		} else {
			// controlled shutdown
			slog.Debug("func HandleCopyStream() exits normally, stopping to receive xlog")
			return nil
		}
	}
}

func closeWalfileNoRename(stream *StreamCtl, notice string) {
	slog.Warn("closing WAL file without renaming", slog.String("cause", notice))
	if stream.walfile != nil {
		err := stream.closeNoRename()
		if err != nil {
			slog.Error("could not close WAL file", slog.Any("err", err))
		}
	}
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
	if err := fsync.Fsync(f); err != nil {
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

func calculateCopyStreamSleepTime(
	stream *StreamCtl,
	now time.Time,
	standbyMessageTimeout time.Duration,
	lastStatus time.Time,
) time.Duration {
	var statusTargetTime time.Time
	var sleepTime time.Duration

	if standbyMessageTimeout > 0 && stream.StillSending {
		statusTargetTime = lastStatus.Add(standbyMessageTimeout)
	}

	if !statusTargetTime.IsZero() {
		diff := statusTargetTime.Sub(now)

		if diff <= 0 {
			// Always sleep at least 1 second if overdue
			sleepTime = 1 * time.Second
		} else {
			sleepTime = diff
		}
	} else {
		sleepTime = -1
	}

	return sleepTime
}
