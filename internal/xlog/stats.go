package xlog

import "time"

type StreamStatus struct {
	Slot         string `json:"slot"`
	Timeline     uint32 `json:"timeline"`
	LastFlushLSN string `json:"last_flush_lsn"`
	Uptime       string `json:"uptime"`
	Running      bool   `json:"running"`
}

func (stream *StreamCtl) Status() *StreamStatus {
	stream.mu.RLock()
	defer stream.mu.RUnlock()
	status := &StreamStatus{
		Slot:         stream.replicationSlot,
		Timeline:     stream.timeline,
		LastFlushLSN: stream.lastFlushPosition.String(),
		Running:      true,
		Uptime:       time.Since(stream.startedAt).Truncate(time.Millisecond).String(),
	}
	return status
}

func (pgrw *PgReceiveWal) Status() *StreamStatus {
	pgrw.streamMu.RLock()
	defer pgrw.streamMu.RUnlock()
	if pgrw.stream == nil {
		return &StreamStatus{
			Running: false,
		}
	}
	return pgrw.stream.Status()
}
