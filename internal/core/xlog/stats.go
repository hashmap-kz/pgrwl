package xlog

import "time"

type StreamStatus struct {
	Slot         string `json:"slot,omitempty"`
	Timeline     uint32 `json:"timeline,omitempty"`
	LastFlushLSN string `json:"last_flush_lsn,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
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

func (pgrw *pgReceiveWal) Status() *StreamStatus {
	pgrw.streamMu.RLock()
	defer pgrw.streamMu.RUnlock()
	if pgrw.stream == nil {
		return &StreamStatus{
			Running: false,
		}
	}
	return pgrw.stream.Status()
}
