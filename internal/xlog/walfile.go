package xlog

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pglogrepl"
)

type walfileT struct {
	currpos  uint64
	pathname string
	fd       *os.File
}

func (stream *StreamCtl) SyncWalFile() error {
	if stream.walfile == nil {
		return fmt.Errorf("stream.walfile is nil, attempt to sync")
	}
	if stream.walfile.fd == nil {
		return fmt.Errorf("stream.walfile.fd is nil, attempt to sync")
	}
	return stream.walfile.fd.Sync()
}

func (stream *StreamCtl) WriteAtWalFile(data []byte, xlogoff int64) error {
	if stream.walfile == nil {
		return fmt.Errorf("stream.walfile is nil, attempt to write")
	}
	n, err := stream.walfile.fd.WriteAt(data, xlogoff)
	if err != nil {
		return err
	}
	if n > 0 {
		stream.walfile.currpos += uint64(n)
	}
	return nil
}

func (stream *StreamCtl) OpenWalFile(startpoint pglogrepl.LSN) error {
	segno := XLByteToSeg(uint64(startpoint), stream.WalSegSz)
	filename := XLogFileName(stream.Timeline, segno, stream.WalSegSz) + stream.PartialSuffix
	fullPath := filepath.Join(stream.BaseDir, filename)

	// Check if file already exists
	stat, err := os.Stat(fullPath)
	if err == nil {
		// File exists
		if uint64(stat.Size()) == stream.WalSegSz {
			// File already correctly sized, open it
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0o660)
			if err != nil {
				return fmt.Errorf("could not open existing WAL file %s: %w", fullPath, err)
			}
			// Fsync to be safe
			if err := fd.Sync(); err != nil {
				fd.Close()
				return fmt.Errorf("could not fsync WAL file %s: %w", fullPath, err)
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
	if err := fd.Truncate(int64(stream.WalSegSz)); err != nil {
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

	// TODO:fsync, simplify, etc...
	var err error
	if strings.HasSuffix(stream.walfile.pathname, ".partial") {
		if stream.walfile.currpos == stream.WalSegSz {
			err = stream.closeAndRename()
		} else {
			err = stream.closeNoRename()
		}
	} else {
		err = stream.closeAndRename()
	}

	stream.LastFlushPosition = pglogrepl.LSN(pos)
	return err
}

func (stream *StreamCtl) closeNoRename() error {
	log.Printf("not renaming \"%s\", segment is not complete", stream.walfile.pathname)
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}
	stream.walfile = nil
	return nil
}

func (stream *StreamCtl) closeAndRename() error {
	log.Printf("renaming \"%s\", segment is complete", stream.walfile.pathname)
	err := stream.walfile.fd.Close()
	if err != nil {
		return err
	}
	err = os.Rename(stream.walfile.pathname, strings.TrimSuffix(stream.walfile.pathname, ".partial"))
	if err != nil {
		return err
	}
	stream.walfile = nil
	return nil
}
