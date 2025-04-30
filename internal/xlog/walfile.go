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
	slog.Debug("OpenWalFile")
	var err error

	segno := XLByteToSeg(uint64(startpoint), stream.WalSegSz)
	filename := XLogFileName(stream.Timeline, segno, stream.WalSegSz) + stream.PartialSuffix
	fullPath := filepath.Join(stream.BaseDir, filename)

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		// File exists
		if conv.ToUint64(stat.Size()) == stream.WalSegSz {
			// File already correctly sized, open it
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0o660)
			if err != nil {
				return fmt.Errorf("could not open existing WAL file %s: %w", fullPath, err)
			}

			// fsync file in case of a previous crash
			if err := fsync.Fsync(fd); err != nil {
				slog.Error("OpenWalFile -> could not fsync existing write-ahead log file. exiting with status 1.",
					slog.String("path", fullPath),
					slog.Any("err", err),
				)
				if err = fd.Close(); err != nil {
					slog.Warn("OpenWalFile -> cannot close file",
						slog.String("path", fullPath),
						slog.Any("err", err),
					)
				}
				if err = os.Remove(fullPath); err != nil {
					slog.Warn("OpenWalFile -> cannot unlink file",
						slog.String("path", fullPath),
						slog.Any("err", err),
					)
				}
				// MARK:exit
				os.Exit(1)
			}

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

	return nil
}

func (stream *StreamCtl) CloseWalfile(pos pglogrepl.LSN) error {
	slog.Debug("CloseWalfile")
	var err error

	if stream.walfile == nil {
		return nil
	}

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

	stream.LastFlushPosition = pos
	return nil
}

func (stream *StreamCtl) closeNoRename() error {
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil (closeNoRename)")
	}

	pathname := stream.walfile.pathname

	slog.Info("close without renaming, segment is not complete", slog.String("path", filepath.ToSlash(pathname)))
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}
	stream.walfile = nil

	slog.Info("fsync filename and parent-directory", slog.String("path", filepath.ToSlash(pathname)))
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

	slog.Info("closing fd", slog.String("path", filepath.ToSlash(pathname)))
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}

	slog.Info("renaming complete segment",
		slog.String("src", filepath.ToSlash(pathname)),
		slog.String("dst", filepath.ToSlash(finalName)),
	)
	err = os.Rename(pathname, finalName)
	if err != nil {
		return err
	}
	stream.walfile = nil

	slog.Info("fsync filename and parent-directory", slog.String("path", filepath.ToSlash(finalName)))
	err = fsync.FsyncFnameAndDir(finalName)
	if err != nil {
		return err
	}
	return nil
}
