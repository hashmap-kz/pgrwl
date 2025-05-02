package xlog

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"pgreceivewal5/internal/fsync"

	"pgreceivewal5/internal/conv"

	"github.com/jackc/pglogrepl"
)

type walfileT struct {
	currpos  uint64
	pathname string
	fd       *os.File
}

func (stream *StreamCtl) SyncWalFile() error {
	if stream.walfile == nil {
		return fmt.Errorf("stream.walfile is nil (SyncWalFile)")
	}
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil (SyncWalFile)")
	}
	return fsync.Fsync(stream.walfile.fd)
}

func (stream *StreamCtl) WriteAtWalFile(data []byte, xlogoff uint64) (int, error) {
	xlogOffToInt64, err := conv.Uint64ToInt64(xlogoff)
	if err != nil {
		return -1, err
	}

	if stream.walfile == nil {
		return -1, fmt.Errorf("stream.walfile is nil (WriteAtWalFile)")
	}
	if stream.walfile.fd == nil {
		return -1, fmt.Errorf("stream.walfile.fd is nil (WriteAtWalFile)")
	}
	n, err := stream.walfile.fd.WriteAt(data, xlogOffToInt64)
	if err != nil {
		return -1, err
	}
	if n > 0 {
		stream.walfile.currpos += uint64(n)
	}
	return n, nil
}

func (stream *StreamCtl) OpenWalFile(startpoint pglogrepl.LSN) error {
	var err error

	segno := XLByteToSeg(uint64(startpoint), stream.WalSegSz)
	filename := XLogFileName(stream.Timeline, segno, stream.WalSegSz) + stream.PartialSuffix
	fullPath := filepath.Join(stream.BaseDir, filename)

	l := slog.With(
		slog.String("job", "OPEN_WAL_FILE"),
		slog.String("startpoint", startpoint.String()),
		slog.String("segno", fmt.Sprintf("%08X", segno)),
		slog.String("path", filepath.ToSlash(fullPath)),
	)

	l.Debug("open WAL file for write")

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		l.Debug("file exists, check size")

		// File exists
		if conv.ToUint64(stat.Size()) == stream.WalSegSz {
			l.Debug("file exists and correctly sized, open")

			// File already correctly sized, open it
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0o660)
			if err != nil {
				return fmt.Errorf("could not open existing WAL file %s: %w", fullPath, err)
			}

			// fsync file in case of a previous crash
			if err := fsync.Fsync(fd); err != nil {
				l.Error("could not fsync existing WAL file. exiting with status 1", slog.Any("err", err))
				if err = fd.Close(); err != nil {
					l.Warn("cannot close file", slog.Any("err", err))
				}
				if err = os.Remove(fullPath); err != nil {
					l.Warn("cannot unlink file", slog.Any("err", err))
				}
				// MARK:exit
				os.Exit(1)
			}

			l.Info("streaming into existing WAL file")
			stream.walfile = &walfileT{
				currpos:  0,
				pathname: fullPath,
				fd:       fd,
			}
			return nil
		}
		if stat.Size() != 0 {
			return fmt.Errorf("corrupt WAL file %s: expected size 0 or %d bytes, found %d",
				fullPath,
				stream.WalSegSz,
				stat.Size(),
			)
		}
		// If size 0, proceed to initialize it
	}

	l.Debug("file does not exists, creating",
		slog.Uint64("size", stream.WalSegSz),
	)

	// Otherwise create new file and preallocate
	fd, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o660)
	if err != nil {
		return fmt.Errorf("could not create WAL file %s: %w", fullPath, err)
	}

	// Preallocate file with zeros up to 16 MiB
	truncateSize, err := conv.Uint64ToInt64(stream.WalSegSz)
	if err != nil {
		return err
	}
	if err := fd.Truncate(truncateSize); err != nil {
		_ = fd.Close()
		return fmt.Errorf("could not preallocate WAL file %s: %w", fullPath, err)
	}

	stream.walfile = &walfileT{
		currpos:  0,
		pathname: fullPath,
		fd:       fd,
	}

	l.Info("new WAL file opened for writing")
	return nil
}

func (stream *StreamCtl) CloseWalfile(pos pglogrepl.LSN) error {
	var err error

	if stream.walfile == nil {
		return nil
	}

	l := slog.With(
		slog.String("job", "CLOSE_WAL_FILE"),
		slog.String("pos", pos.String()),
		slog.String("path", filepath.ToSlash(stream.walfile.pathname)),
	)
	l.Debug("close WAL file")

	if strings.HasSuffix(stream.walfile.pathname, stream.PartialSuffix) {
		if stream.walfile.currpos == stream.WalSegSz {
			err = stream.closeAndRename()
		} else {
			err = stream.closeNoRename()
		}
	} else {
		err = stream.closeAndRename()
	}

	if err != nil {
		slog.Error("could not close file, (CloseWalfile)", slog.Any("err", err))
		return fmt.Errorf("could not close file: %w", err)
	}

	stream.updateLastFlushPosition(pos, "CloseWalfile")
	return nil
}

func (stream *StreamCtl) closeNoRename() error {
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil (closeNoRename)")
	}

	pathname := stream.walfile.pathname
	l := slog.With(
		slog.String("job", "CLOSE_WAL_FILE_NO_RENAME"),
		slog.String("path", filepath.ToSlash(pathname)),
	)

	l.Warn("close without renaming, segment is not complete")
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}
	stream.walfile = nil

	l.Debug("fsync filename and parent-directory")
	err = fsync.FsyncFnameAndDir(pathname)
	if err != nil {
		return err
	}
	return nil
}

func (stream *StreamCtl) closeAndRename() error {
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil (closeAndRename)")
	}

	pathname := stream.walfile.pathname
	finalName := strings.TrimSuffix(pathname, stream.PartialSuffix)
	l := slog.With(
		slog.String("job", "CLOSE_WAL_FILE_WITH_RENAME"),
		slog.String("src", filepath.ToSlash(pathname)),
		slog.String("dst", filepath.ToSlash(finalName)),
	)

	l.Debug("closing fd")
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}

	l.Debug("renaming complete segment")
	err = os.Rename(pathname, finalName)
	if err != nil {
		return err
	}
	stream.walfile = nil

	l.Debug("fsync filename and parent-directory")
	err = fsync.FsyncFnameAndDir(finalName)
	if err != nil {
		return err
	}

	l.Info("segment is complete")
	return nil
}
