package xlog

import (
	"fmt"
)

// TODO:query it
const (
	WalSegSz = 16 * 1024 * 1024 // PostgreSQL default 16MiB
)

// https://github.com/postgres/postgres/blob/master/src/include/access/xlog_internal.h

func XLByteToSeg(xlrp uint64, walSegSize uint64) uint64 {
	return uint64(xlrp) / walSegSize
}

func XLogSegmentOffset(xlogptr uint64, walSegSize uint64) int {
	return int(xlogptr & (walSegSize - 1))
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
