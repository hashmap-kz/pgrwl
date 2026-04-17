package receivemode

import "github.com/pgrwl/pgrwl/internal/core/xlog"

// ReceiverStatus is the top-level response for GET /receiver and GET /status.
type ReceiverStatus struct {
	// State is the lifecycle state of the WAL receiver: "running" or "stopped".
	State xlog.ReceiverState `json:"state"`
	// Stream is populated only when State is "running".
	Stream *StreamStatus `json:"stream,omitempty"`
}

// StreamStatus mirrors xlog.StreamStatus with JSON field names suitable for
// the HTTP API.
type StreamStatus struct {
	Slot         string `json:"slot,omitempty"`
	Timeline     uint32 `json:"timeline,omitempty"`
	LastFlushLSN string `json:"last_flush_lsn,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
	Running      bool   `json:"running"`
}

// ReceiverStateRequest is the JSON body for POST /receiver.
type ReceiverStateRequest struct {
	// State must be "running" or "stopped".
	State xlog.ReceiverState `json:"state"`
}

// BriefConfig is returned by GET /config.
type BriefConfig struct {
	RetentionEnable bool `json:"retention_enable"`
}
