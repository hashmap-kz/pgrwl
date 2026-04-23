package receivemode

import "time"

type StreamStatus struct {
	Slot         string `json:"slot,omitempty"`
	Timeline     uint32 `json:"timeline,omitempty"`
	LastFlushLSN string `json:"last_flush_lsn,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
	Running      bool   `json:"running"`
}

// PgrwlStatus is the top-level status response for the receiver service.
type PgrwlStatus struct {
	RunningMode  string        `json:"running_mode"`
	StreamStatus *StreamStatus `json:"stream_status,omitempty"`
}

// BriefConfig exposes a minimal, UI-friendly subset of the active config.
type BriefConfig struct {
	RetentionEnable bool `json:"retention_enable"`
}

// Receiver represents the WAL receiver runtime identity.
type Receiver struct {
	Slot         string `json:"slot,omitempty"`
	Timeline     uint32 `json:"timeline,omitempty"`
	LastFlushLSN string `json:"last_flush_lsn,omitempty"`
	Uptime       string `json:"uptime,omitempty"`
	Running      bool   `json:"running"`
}

// WALFile is a single entry in the WAL archive as seen by the UI.
type WALFile struct {
	Name       string    `json:"name"`
	Ext        string    `json:"ext"`
	Filename   string    `json:"filename"`
	SizeMB     float64   `json:"size_mb"`
	UploadedAt time.Time `json:"uploaded_at"`
	Encrypted  bool      `json:"encrypted"`
}

// Backup represents a completed (or in-progress) base backup.
type Backup struct {
	Label       string    `json:"label"`
	Started     time.Time `json:"started"`
	Finished    time.Time `json:"finished"`
	SizeGB      float64   `json:"size_gb"`
	WALStartLSN string    `json:"wal_start_lsn"`
	WALStopLSN  string    `json:"wal_stop_lsn"`
	Status      string    `json:"status"`
}

// Snapshot is the composite payload returned by GET /api/v1/snapshot.
// It bundles all information the UI dashboard needs in a single round-trip.
type Snapshot struct {
	Receiver Receiver     `json:"receiver"`
	Status   *PgrwlStatus `json:"status"`
	Config   *BriefConfig `json:"config"`
	WALFiles []WALFile    `json:"wal_files"`
	Backups  []Backup     `json:"backups"`
	Error    string       `json:"error,omitempty"`
}
