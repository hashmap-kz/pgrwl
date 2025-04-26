package xlog

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

var stillSending = true

// TODO:query it
const walSegSz = 16 * 1024 * 1024 // PostgreSQL default 16MiB
const xLogSegmentsPerXLogID = uint64(0x100000000) / walSegSz

type StreamCtl struct {
	StartPos               pglogrepl.LSN
	Timeline               uint32
	StandbyMessageTimeout  time.Duration
	Synchronous            bool
	StopSocket             int // we can ignore this for now
	StreamStop             func(blockpos pglogrepl.LSN, timeline uint32, endOfSegment bool) bool
	SendFeedback           func(conn *pgconn.PgConn, blockpos pglogrepl.LSN, now time.Time, replyRequested bool) error
	FlushWAL               func() error
	WriteXLogData          func(xld *pglogrepl.XLogData) error
	WriteKeepaliveResponse func() error
}

func openWalFile(dir string, timeline uint32, startLSN pglogrepl.LSN) (*os.File, string, error) {

	segno := uint64(startLSN) / walSegSz
	filename := fmt.Sprintf("%08X%08X%08X.partial", timeline, segno/xLogSegmentsPerXLogID, segno%xLogSegmentsPerXLogID)
	fullPath := filepath.Join(dir, filename)

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		// File exists
		if stat.Size() == walSegSz {
			// File already correctly sized, open it
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0660)
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
			return nil, "", fmt.Errorf("corrupt WAL file %s: expected size 0 or %d bytes, found %d", fullPath, walSegSz, stat.Size())
		}
		// If size 0, proceed to initialize it
	}

	// Otherwise create new file and preallocate
	fd, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0660)
	if err != nil {
		return nil, "", fmt.Errorf("could not create WAL file %s: %w", fullPath, err)
	}

	// Preallocate file with zeros up to 16 MiB
	if err := fd.Truncate(int64(walSegSz)); err != nil {
		fd.Close()
		return nil, "", fmt.Errorf("could not preallocate WAL file %s: %w", fullPath, err)
	}

	return fd, fullPath, nil
}

func ProcessXLogDataMsg(
	conn *pgconn.PgConn,
	stream *StreamCtl,
	copybuf []byte,
	blockpos *pglogrepl.LSN,
	seg **walSegment, // <-- pass current segment reference
) (bool, error) {

	if len(copybuf) < 1+8+8+8 {
		return false, fmt.Errorf("streaming header too small: %d", len(copybuf))
	}

	dataStart := pglogrepl.LSN(binary.BigEndian.Uint64(copybuf[1:9]))
	// walEnd := pglogrepl.LSN(binary.BigEndian.Uint64(copybuf[9:17]))
	// sendTime := int64(binary.BigEndian.Uint64(copybuf[17:25]))

	xlogoff := int(uint64(dataStart) % walSegSz)
	data := copybuf[25:] // actual WAL data

	// If no open file, expect offset to be zero
	if *seg == nil {
		if xlogoff != 0 {
			return false, fmt.Errorf("received WAL at offset %d but no file open", xlogoff)
		}
		newSeg := newWalSegment(stream.timeline, dataStart)
		*seg = newSeg
	}

	// Check we are writing exactly at the expected position
	curSize, err := (*seg).fd.Seek(0, os.SEEK_END)
	if err != nil {
		return false, fmt.Errorf("could not seek current WAL file: %w", err)
	}
	if int(curSize) != xlogoff {
		return false, fmt.Errorf("unexpected WAL offset: got %08x, expected %08x", xlogoff, curSize)
	}

	bytesLeft := len(data)
	bytesWritten := 0

	for bytesLeft > 0 {
		bytesToWrite := bytesLeft
		if xlogoff+bytesLeft > walSegSz {
			bytesToWrite = walSegSz - xlogoff
		}

		_, err := (*seg).fd.WriteAt(data[bytesWritten:bytesWritten+bytesToWrite], int64(xlogoff))
		if err != nil {
			return false, fmt.Errorf("could not write %d bytes to WAL file: %w", bytesToWrite, err)
		}

		bytesWritten += bytesToWrite
		bytesLeft -= bytesToWrite
		*blockpos += pglogrepl.LSN(bytesToWrite)
		xlogoff += bytesToWrite

		// If we completed a WAL segment
		if xlogoff == int(walSegSz) {
			if err := (*seg).flush(); err != nil {
				return false, fmt.Errorf("could not flush WAL file: %w", err)
			}
			if err := (*seg).closeAndRename(); err != nil {
				return false, fmt.Errorf("could not close WAL file: %w", err)
			}

			// prepare next WAL segment
			newSeg, _ := newWalSegment(stream.Timeline, *blockpos)
			*seg = newSeg

			// Check if we should stop
			if stream.StreamStop != nil && stream.StreamStop(*blockpos, stream.Timeline, true) {
				// Send CopyEnd
				_, _ = pglogrepl.SendStandbyCopyDone(context.Background(), conn)
				return true, nil
			}
			xlogoff = 0
		}
	}
	return true, nil
}

func HandleCopyStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl) (pglogrepl.LSN, error) {
	lastStatusTime := time.Time{}
	blockPos := stream.StartPos

	stillSending = true

	for {
		// Check if we should stop
		if stillSending && stream.StreamStop(blockPos, stream.Timeline, false) {
			// gracefully end
			return blockPos, nil
		}

		now := time.Now()

		// If synchronous, flush + feedback immediately
		if stream.Synchronous {
			if err := stream.FlushWAL(); err != nil {
				return 0, fmt.Errorf("flush WAL failed: %w", err)
			}
			if err := stream.SendFeedback(conn, blockPos, now, false); err != nil {
				return 0, fmt.Errorf("send feedback failed: %w", err)
			}
			lastStatusTime = now
		}

		// If timeout elapsed, send a standby status update
		if stream.StandbyMessageTimeout > 0 &&
			now.After(lastStatusTime.Add(stream.StandbyMessageTimeout)) {
			if err := stream.SendFeedback(conn, blockPos, now, false); err != nil {
				return 0, fmt.Errorf("send feedback timed out: %w", err)
			}
			lastStatusTime = now
		}

		// Calculate how long we can block
		sleepTime := calculateSleepTimeout(now, lastStatusTime, stream.StandbyMessageTimeout)

		// Receive WAL from the server
		ctxWithTimeout, cancel := context.WithTimeout(ctx, sleepTime)
		msg, err := conn.ReceiveMessage(ctxWithTimeout)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				continue // normal, timed out waiting
			}
			return 0, fmt.Errorf("receive failed: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.CopyData:
			switch m.Data[0] {
			case pglogrepl.PrimaryKeepaliveMessageByteID:
				keepalive, err := pglogrepl.ParsePrimaryKeepaliveMessage(m.Data[1:])
				if err != nil {
					return 0, fmt.Errorf("parse keepalive failed: %w", err)
				}
				if keepalive.ReplyRequested {
					if err := stream.SendFeedback(conn, blockPos, now, false); err != nil {
						return 0, fmt.Errorf("keepalive feedback failed: %w", err)
					}
					lastStatusTime = now
				}

			case pglogrepl.XLogDataByteID:
				xld, err := pglogrepl.ParseXLogData(m.Data[1:])
				if err != nil {
					return 0, fmt.Errorf("parse XLogData failed: %w", err)
				}
				if err := stream.WriteXLogData(&xld); err != nil {
					return 0, fmt.Errorf("writing xlogdata failed: %w", err)
				}
				blockPos = xld.WALStart + pglogrepl.LSN(len(xld.WALData))

				// Recheck if we should stop after writing
				if stillSending && stream.StreamStop(blockPos, stream.Timeline, true) {
					return blockPos, nil
				}

			default:
				return 0, fmt.Errorf("unexpected CopyData message type: %v", m.Data[0])
			}

		case *pgproto3.CopyDone:
			// Server indicates end of stream
			return blockPos, nil

		default:
			return 0, fmt.Errorf("unexpected server message %T", msg)
		}
	}
}

// Helper: How long to wait for next standby_message_timeout
func calculateSleepTimeout(now, lastStatus time.Time, standbyMessageTimeout time.Duration) time.Duration {
	if standbyMessageTimeout <= 0 {
		return time.Second * 10 // default
	}
	next := lastStatus.Add(standbyMessageTimeout)
	if next.After(now) {
		return next.Sub(now)
	}
	return time.Second
}
