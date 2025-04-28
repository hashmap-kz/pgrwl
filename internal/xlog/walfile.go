package xlog

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	return stream.walfile.fd.Sync()
}

func (stream *StreamCtl) WriteAtWalFile(data []byte, xlogoff uint64) error {
	xlogOffToInt64, err := conv.Uint64ToInt64(xlogoff)
	if err != nil {
		return err
	}

	if stream.walfile == nil {
		return fmt.Errorf("stream.walfile is nil (WriteAtWalFile)")
	}
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil (WriteAtWalFile)")
	}
	n, err := stream.walfile.fd.WriteAt(data, xlogOffToInt64)
	if err != nil {
		return err
	}
	if n > 0 {
		stream.walfile.currpos += uint64(n)
	}
	return nil
}

func (stream *StreamCtl) OpenWalFile(startpoint pglogrepl.LSN) error {
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
			if err := fd.Sync(); err != nil {
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
		fd.Close()
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
	if stream.walfile == nil {
		return nil
	}

	var err error
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

	log.Printf("not renaming \"%s\", segment is not complete", pathname)
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}
	stream.walfile = nil

	log.Printf("fsync filename and parent-directory: %s", pathname)
	err = FsyncFname(pathname)
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

	log.Printf("closing file: %s", pathname)
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}

	log.Printf("renaming %s to %s, segment is complete", pathname, finalName)
	err = os.Rename(pathname, finalName)
	if err != nil {
		return err
	}
	stream.walfile = nil

	log.Printf("fsync filename and parent-directory: %s", finalName)
	err = FsyncFname(finalName)
	if err != nil {
		return err
	}
	return nil
}
