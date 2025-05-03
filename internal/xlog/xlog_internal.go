package xlog

import (
	"fmt"
	"strconv"

	"github.com/jackc/pglogrepl"
)

// https://github.com/postgres/postgres/blob/master/src/include/access/xlog_internal.h

type (
	TimeLineID uint32
	//nolint:revive
	XLogSegNo uint64
)

const (
	WalSegMinSize = 1 * 1024 * 1024        // 1 MiB
	WalSegMaxSize = 1 * 1024 * 1024 * 1024 // 1 GiB

	XLogFileNameLen = 24
	partialSuffix   = ".partial"
)

// IsPowerOf2 returns true if x is a power of 2
func IsPowerOf2(x uint64) bool {
	return x > 0 && (x&(x-1)) == 0
}

// IsValidWalSegSize checks if size is a valid wal_segment_size (1MiB..1GiB and power of 2)
func IsValidWalSegSize(size uint64) bool {
	return IsPowerOf2(size) && (size >= WalSegMinSize && size <= WalSegMaxSize)
}

func XLByteToSeg(xlrp, walSegSize uint64) uint64 {
	return xlrp / walSegSize
}

//nolint:revive
func XLogSegmentOffset(xlogptr pglogrepl.LSN, walSegSize uint64) uint64 {
	return uint64(xlogptr) & (walSegSize - 1)
}

//nolint:revive
func XLogSegmentsPerXLogId(walSegSize uint64) uint64 {
	return 0x100000000 / walSegSize
}

//nolint:revive
func XLogSegNoOffsetToRecPtr(segno XLogSegNo, offset uint32, walSegSize uint64) pglogrepl.LSN {
	return pglogrepl.LSN(uint64(segno)*walSegSize + uint64(offset))
}

//nolint:revive
func XLogFileName(tli uint32, logSegNo, walSegSize uint64) string {
	hi := logSegNo / XLogSegmentsPerXLogId(walSegSize)
	lo := logSegNo % XLogSegmentsPerXLogId(walSegSize)
	return fmt.Sprintf("%08X%08X%08X", tli, hi, lo)
}

// wal file names

var hexSet = map[rune]bool{
	'0': true, '1': true, '2': true, '3': true,
	'4': true, '5': true, '6': true, '7': true,
	'8': true, '9': true, 'A': true, 'B': true,
	'C': true, 'D': true, 'E': true, 'F': true,
}

func strspnMap(s string, valid map[rune]bool) int {
	count := 0
	for _, c := range s {
		if !valid[c] {
			break
		}
		count++
	}
	return count
}

func IsXLogFileNameManual(fname string) bool {
	return len(fname) == XLogFileNameLen &&
		strspnMap(fname, hexSet) == XLogFileNameLen
}

func IsPartialXLogFileName(fname string) bool {
	expectedLen := XLogFileNameLen + len(partialSuffix)

	return len(fname) == expectedLen &&
		strspnMap(fname[:XLogFileNameLen], hexSet) == XLogFileNameLen &&
		fname[XLogFileNameLen:] == partialSuffix
}

// XLogFromFileName parses a 24-character WAL segment filename.
//
//nolint:revive
func XLogFromFileName(fname string, walSegSize uint64) (tli TimeLineID, logSegNo XLogSegNo, err error) {
	if len(fname) < 24 {
		return 0, 0, fmt.Errorf("WAL filename too short: %s", fname)
	}

	tliHex := fname[0:8]
	logHex := fname[8:16]
	segHex := fname[16:24]

	tli64, err := strconv.ParseUint(tliHex, 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid TLI: %w", err)
	}
	log, err := strconv.ParseUint(logHex, 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid log ID: %w", err)
	}
	seg, err := strconv.ParseUint(segHex, 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid segment ID: %w", err)
	}

	segmentsPerXlogID := XLogSegmentsPerXLogId(walSegSize)
	logSegNo = XLogSegNo(log)*XLogSegNo(segmentsPerXlogID) + XLogSegNo(seg)

	return TimeLineID(tli64), logSegNo, nil
}
