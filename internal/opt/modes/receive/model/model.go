package model

type StreamStatus struct {
	Slot         string `json:"slot,omitempty"`
	Timeline     uint32 `json:"timeline,omitempty"`
	LastFlushLSN string `json:"last_flush_lsn,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
	Running      bool   `json:"running"`
}

type PgRwlStatus struct {
	RunningMode  string        `json:"running_mode"`
	StreamStatus *StreamStatus `json:"stream_status,omitempty"`
}
