package xlog

import "fmt"

// https://github.com/postgres/postgres/blob/master/src/include/access/xlog_internal.h

func XLByteToSeg(xlrp uint64, walSegSize uint64) uint64 {
	return xlrp / walSegSize
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
