package xlog

import (
	"fmt"

	"github.com/jackc/pglogrepl"
)

// xlog_internal

const (
	WalSegMinSize = 1 * 1024 * 1024        // 1 MiB
	WalSegMaxSize = 1 * 1024 * 1024 * 1024 // 1 GiB
)

// IsPowerOf2 returns true if x is a power of 2
func IsPowerOf2(x uint64) bool {
	return x > 0 && (x&(x-1)) == 0
}

// IsValidWalSegSize checks if size is a valid wal_segment_size (1MiB..1GiB and power of 2)
func IsValidWalSegSize(size uint64) bool {
	return IsPowerOf2(size) && (size >= WalSegMinSize && size <= WalSegMaxSize)
}

// https://github.com/postgres/postgres/blob/master/src/include/access/xlog_internal.h

func XLByteToSeg(xlrp uint64, walSegSize uint64) uint64 {
	return uint64(xlrp) / walSegSize
}

func XLogSegmentOffset(xlogptr pglogrepl.LSN, walSegSize uint64) uint64 {
	return uint64(xlogptr) & (walSegSize - 1)
}

func XLByteToPrevSeg(xlrp uint64, walSegSize uint64) uint64 {
	return (xlrp - 1) / walSegSize
}

func XLogSegmentsPerXLogId(walSegSize uint64) uint64 {
	return 0x100000000 / walSegSize
}

func XLogFileName(tli uint32, logSegNo uint64, walSegSize uint64) string {
	return fmt.Sprintf("%08X%08X%08X",
		tli,
		uint32(logSegNo/XLogSegmentsPerXLogId(walSegSize)),
		uint32(logSegNo%XLogSegmentsPerXLogId(walSegSize)),
	)
}
