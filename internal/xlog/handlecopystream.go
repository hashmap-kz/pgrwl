package xlog

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

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

func handleCopyStream(ctx context.Context, conn *pgconn.PgConn, stream *StreamCtl) error {
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

				if _, err := ProcessXLogDataMsg(conn, stream, xld.WALData, &bp); err != nil {
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
