package xlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pglogrepl"
)

type walSegment struct {
	tli      uint32
	startLSN pglogrepl.LSN
	endLSN   pglogrepl.LSN
	writeLSN pglogrepl.LSN // up to where we've written
	buf      []byte
	writeIdx int
	fd       *os.File
	filePath string
	baseDir  string
}

func newWalSegment(baseDir string, tli uint32, startLSN pglogrepl.LSN) (*walSegment, error) {
	const walSegSize = 16 * 1024 * 1024

	segStart := pglogrepl.LSN(uint64(startLSN) / walSegSize * walSegSize)

	ws := &walSegment{
		tli:      tli,
		startLSN: segStart,
		endLSN:   segStart + pglogrepl.LSN(walSegSize),
		buf:      make([]byte, walSegSize),
		baseDir:  baseDir,
	}

	if err := ws.openOrCreate(); err != nil {
		return nil, err
	}

	return ws, nil
}

// openOrCreate opens or creates the .partial WAL file, ensuring it is 16MiB sized
func (ws *walSegment) openOrCreate() error {
	const walSegSize = 16 * 1024 * 1024

	segno := uint64(ws.startLSN) / walSegSize
	filename := fmt.Sprintf("%08X%08X%08X.partial", ws.tli, segno/xLogSegmentsPerXLogID, segno%xLogSegmentsPerXLogID)
	fullPath := filepath.Join(ws.baseDir, filename)

	if err := os.MkdirAll(ws.baseDir, 0750); err != nil {
		return err
	}

	stat, err := os.Stat(fullPath)
	if err == nil {
		if stat.Size() == walSegSize {
			fd, err := os.OpenFile(fullPath, os.O_RDWR, 0660)
			if err != nil {
				return fmt.Errorf("could not open WAL file %s: %w", fullPath, err)
			}
			if err := fd.Sync(); err != nil {
				fd.Close()
				return fmt.Errorf("could not fsync WAL file %s: %w", fullPath, err)
			}
			ws.fd = fd
			ws.filePath = fullPath
			ws.writeIdx = int(stat.Size()) // Should be walSegSize
			ws.writeLSN = ws.startLSN + pglogrepl.LSN(ws.writeIdx)
			return nil
		}
		if stat.Size() != 0 {
			return fmt.Errorf("corrupt WAL file %s: size %d unexpected", fullPath, stat.Size())
		}
		// size 0 - fall through
	}

	fd, err := os.OpenFile(fullPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0660)
	if err != nil {
		return fmt.Errorf("could not create WAL file %s: %w", fullPath, err)
	}
	if err := fd.Truncate(int64(walSegSize)); err != nil {
		fd.Close()
		return fmt.Errorf("could not preallocate WAL file %s: %w", fullPath, err)
	}

	ws.fd = fd
	ws.filePath = fullPath
	ws.writeIdx = 0
	ws.writeLSN = ws.startLSN
	return nil
}

func (ws *walSegment) writeAt(offset int, data []byte) error {
	if ws.fd == nil {
		return fmt.Errorf("WAL file not open")
	}
	if offset+len(data) > len(ws.buf) {
		return fmt.Errorf("WAL write would overflow segment")
	}
	copy(ws.buf[offset:], data)
	ws.writeIdx = offset + len(data)
	ws.writeLSN = ws.startLSN + pglogrepl.LSN(ws.writeIdx)
	return nil
}

func (ws *walSegment) flush() error {
	if ws.fd == nil {
		return nil
	}
	if _, err := ws.fd.WriteAt(ws.buf[:ws.writeIdx], 0); err != nil {
		return err
	}
	return ws.fd.Sync()
}

func (ws *walSegment) closeAndRename() error {
	if ws.fd == nil {
		return nil
	}
	if err := ws.flush(); err != nil {
		return err
	}
	if err := ws.fd.Close(); err != nil {
		return err
	}
	finalName := strings.TrimSuffix(ws.filePath, ".partial")
	if err := os.Rename(ws.filePath, finalName); err != nil {
		return err
	}
	ws.fd = nil
	ws.filePath = ""
	return nil
}

func (ws *walSegment) isComplete() bool {
	return ws.writeIdx == len(ws.buf)
}
