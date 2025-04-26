package xlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// https://github.com/postgres/postgres/blob/master/src/bin/pg_basebackup/receivelog.c

var stillSending = true

type walfileT struct {
	currpos  int
	pathname string
	fd       *os.File
}

var walfile *walfileT

// TODO:query it
const (
	walSegSz              = 16 * 1024 * 1024 // PostgreSQL default 16MiB
	xLogSegmentsPerXLogID = uint64(0x100000000) / walSegSz
)

type StreamCtl struct {
	StartPos               pglogrepl.LSN
	Timeline               uint32
	StandbyMessageTimeout  time.Duration
	Synchronous            bool
	PartialSuffix          string
	StreamStop             func(blockpos pglogrepl.LSN, timeline uint32, endOfSegment bool) bool
	SendFeedback           func(conn *pgconn.PgConn, blockpos pglogrepl.LSN, now time.Time, replyRequested bool) error
	FlushWAL               func() error
	WriteXLogData          func(xld *pglogrepl.XLogData) error
	WriteKeepaliveResponse func() error
}

/*
//  * Open a new WAL file in the specified directory.
//  *
//  * Returns true if OK; on failure, returns false after printing an error msg.
//  * On success, 'walfile' is set to the opened WAL file.
//  *
//  * The file will be padded to 16Mb with zeroes.
//  */
// static bool
// open_walfile(StreamCtl *stream, XLogRecPtr startpoint)
// {
// 	Walfile    *f;
// 	char	   *fn;
// 	ssize_t		size;
// 	XLogSegNo	segno;
// 	char		walfile_name[MAXPGPATH];
//
// 	XLByteToSeg(startpoint, segno, WalSegSz);
// 	XLogFileName(walfile_name, stream->timeline, segno, WalSegSz);
//
// 	/* Note that this considers the compression used if necessary */
// 	fn = stream->walmethod->ops->get_file_name(stream->walmethod,
// 											   walfile_name,
// 											   stream->partial_suffix);
//
// 	/*
// 	 * When streaming to files, if an existing file exists we verify that it's
// 	 * either empty (just created), or a complete WalSegSz segment (in which
// 	 * case it has been created and padded). Anything else indicates a corrupt
// 	 * file. Compressed files have no need for padding, so just ignore this
// 	 * case.
// 	 *
// 	 * When streaming to tar, no file with this name will exist before, so we
// 	 * never have to verify a size.
// 	 */
// 	if (stream->walmethod->compression_algorithm == PG_COMPRESSION_NONE &&
// 		stream->walmethod->ops->existsfile(stream->walmethod, fn))
// 	{
// 		size = stream->walmethod->ops->get_file_size(stream->walmethod, fn);
// 		if (size < 0)
// 		{
// 			pg_log_error("could not get size of write-ahead log file \"%s\": %s",
// 						 fn, GetLastWalMethodError(stream->walmethod));
// 			pg_free(fn);
// 			return false;
// 		}
// 		if (size == WalSegSz)
// 		{
// 			/* Already padded file. Open it for use */
// 			f = stream->walmethod->ops->open_for_write(stream->walmethod, walfile_name, stream->partial_suffix, 0);
// 			if (f == NULL)
// 			{
// 				pg_log_error("could not open existing write-ahead log file \"%s\": %s",
// 							 fn, GetLastWalMethodError(stream->walmethod));
// 				pg_free(fn);
// 				return false;
// 			}
//
// 			/* fsync file in case of a previous crash */
// 			if (stream->walmethod->ops->sync(f) != 0)
// 			{
// 				pg_log_error("could not fsync existing write-ahead log file \"%s\": %s",
// 							 fn, GetLastWalMethodError(stream->walmethod));
// 				stream->walmethod->ops->close(f, CLOSE_UNLINK);
// 				exit(1);
// 			}
//
// 			walfile = f;
// 			pg_free(fn);
// 			return true;
// 		}
// 		if (size != 0)
// 		{
// 			/* if write didn't set errno, assume problem is no disk space */
// 			if (errno == 0)
// 				errno = ENOSPC;
// 			pg_log_error(ngettext("write-ahead log file \"%s\" has %zd byte, should be 0 or %d",
// 								  "write-ahead log file \"%s\" has %zd bytes, should be 0 or %d",
// 								  size),
// 						 fn, size, WalSegSz);
// 			pg_free(fn);
// 			return false;
// 		}
// 		/* File existed and was empty, so fall through and open */
// 	}
//
// 	/* No file existed, so create one */
//
// 	f = stream->walmethod->ops->open_for_write(stream->walmethod,
// 											   walfile_name,
// 											   stream->partial_suffix,
// 											   WalSegSz);
// 	if (f == NULL)
// 	{
// 		pg_log_error("could not open write-ahead log file \"%s\": %s",
// 					 fn, GetLastWalMethodError(stream->walmethod));
// 		pg_free(fn);
// 		return false;
// 	}
//
// 	pg_free(fn);
// 	walfile = f;
// 	return true;
// }

func openWalFile(stream *StreamCtl, startpoint uint64) (*os.File, string, error) {
	segno := XLByteToSeg(startpoint, walSegSz)
	filename := XLogFileName(stream.Timeline, segno, walSegSz) + stream.PartialSuffix
	// TODO:fix
	fullPath := filepath.Join("wals", filename)

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		// File exists
		if stat.Size() == walSegSz {
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
			return nil, "", fmt.Errorf("corrupt WAL file %s: expected size 0 or %d bytes, found %d", fullPath, walSegSz, stat.Size())
		}
		// If size 0, proceed to initialize it
	}

	// Otherwise create new file and preallocate
	fd, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o660)
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

func closeWalfile(stream *StreamCtl, pos uint64) error {
	return nil
}

// /*
//  * Process XLogData message.
//  */
// static bool
// ProcessXLogDataMsg(PGconn *conn, StreamCtl *stream, char *copybuf, int len,
// 				   XLogRecPtr *blockpos)
// {
// 	int			xlogoff;
// 	int			bytes_left;
// 	int			bytes_written;
// 	int			hdr_len;
//
// 	/*
// 	 * Once we've decided we don't want to receive any more, just ignore any
// 	 * subsequent XLogData messages.
// 	 */
// 	if (!(still_sending))
// 		return true;
//
// 	/*
// 	 * Read the header of the XLogData message, enclosed in the CopyData
// 	 * message. We only need the WAL location field (dataStart), the rest of
// 	 * the header is ignored.
// 	 */
// 	hdr_len = 1;				/* msgtype 'w' */
// 	hdr_len += 8;				/* dataStart */
// 	hdr_len += 8;				/* walEnd */
// 	hdr_len += 8;				/* sendTime */
// 	if (len < hdr_len)
// 	{
// 		pg_log_error("streaming header too small: %d", len);
// 		return false;
// 	}
// 	*blockpos = fe_recvint64(&copybuf[1]);
//
// 	/* Extract WAL location for this block */
// 	xlogoff = XLogSegmentOffset(*blockpos, WalSegSz);
//
// 	/*
// 	 * Verify that the initial location in the stream matches where we think
// 	 * we are.
// 	 */
// 	if (walfile == NULL)
// 	{
// 		/* No file open yet */
// 		if (xlogoff != 0)
// 		{
// 			pg_log_error("received write-ahead log record for offset %u with no file open",
// 						 xlogoff);
// 			return false;
// 		}
// 	}
// 	else
// 	{
// 		/* More data in existing segment */
// 		if (walfile->currpos != xlogoff)
// 		{
// 			pg_log_error("got WAL data offset %08x, expected %08x",
// 						 xlogoff, (int) walfile->currpos);
// 			return false;
// 		}
// 	}
//
// 	bytes_left = len - hdr_len;
// 	bytes_written = 0;
//
// 	while (bytes_left)
// 	{
// 		int			bytes_to_write;
//
// 		/*
// 		 * If crossing a WAL boundary, only write up until we reach wal
// 		 * segment size.
// 		 */
// 		if (xlogoff + bytes_left > WalSegSz)
// 			bytes_to_write = WalSegSz - xlogoff;
// 		else
// 			bytes_to_write = bytes_left;
//
// 		if (walfile == NULL)
// 		{
// 			if (!open_walfile(stream, *blockpos))
// 			{
// 				/* Error logged by open_walfile */
// 				return false;
// 			}
// 		}
//
// 		if (stream->walmethod->ops->write(walfile,
// 										  copybuf + hdr_len + bytes_written,
// 										  bytes_to_write) != bytes_to_write)
// 		{
// 			pg_log_error("could not write %d bytes to WAL file \"%s\": %s",
// 						 bytes_to_write, walfile->pathname,
// 						 GetLastWalMethodError(stream->walmethod));
// 			return false;
// 		}
//
// 		/* Write was successful, advance our position */
// 		bytes_written += bytes_to_write;
// 		bytes_left -= bytes_to_write;
// 		*blockpos += bytes_to_write;
// 		xlogoff += bytes_to_write;
//
// 		/* Did we reach the end of a WAL segment? */
// 		if (XLogSegmentOffset(*blockpos, WalSegSz) == 0)
// 		{
// 			if (!close_walfile(stream, *blockpos))
// 				/* Error message written in close_walfile() */
// 				return false;
//
// 			xlogoff = 0;
//
// 			if (still_sending && stream->stream_stop(*blockpos, stream->timeline, true))
// 			{
// 				if (PQputCopyEnd(conn, NULL) <= 0 || PQflush(conn))
// 				{
// 					pg_log_error("could not send copy-end packet: %s",
// 								 PQerrorMessage(conn));
// 					return false;
// 				}
// 				still_sending = false;
// 				return true;	/* ignore the rest of this XLogData packet */
// 			}
// 		}
// 	}
// 	/* No more data left to write, receive next copy packet */
//
// 	return true;
// }

func ProcessXLogDataMsg(
	conn *pgconn.PgConn,
	stream *StreamCtl,
	copybuf []byte,
	blockpos *uint64,
) (bool, error) {
	/*
	 * Once we've decided we don't want to receive any more, just ignore any
	 * subsequent XLogData messages.
	 */
	if !stillSending {
		return true, nil
	}

	if len(copybuf) < 1+8+8+8 {
		return false, fmt.Errorf("streaming header too small: %d", len(copybuf))
	}

	xld, err := pglogrepl.ParseXLogData(copybuf[1:])
	if err != nil {
		return false, err
	}

	xlogoff := XLogSegmentOffset(uint64(xld.WALStart), walSegSz)

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
		if xlogoff+bytesLeft > walSegSz {
			bytesToWrite = int(walSegSz - xlogoff)
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

		if XLogSegmentOffset(*blockpos, walSegSz) == 0 {
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
