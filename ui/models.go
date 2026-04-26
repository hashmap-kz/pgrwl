package ui

import "time"

type Receiver struct {
	Label string
	Addr  string
}

type StreamStatus struct {
	Slot         string `json:"slot"`
	Timeline     int    `json:"timeline"`
	LastFlushLSN string `json:"last_flush_lsn"`
	Uptime       string `json:"uptime"`
	Running      bool   `json:"running"`
}

type PgrwlStatus struct {
	RunningMode  string        `json:"running_mode"`
	StreamStatus *StreamStatus `json:"stream_status"`
}

type BriefConfig struct {
	RetentionEnable bool `json:"retention_enable"`
}

type WALFile struct {
	Name       string    `json:"name"`
	Ext        string    `json:"ext"`
	Filename   string    `json:"filename"`
	SizeMB     float64   `json:"size_mb"`
	UploadedAt time.Time `json:"uploaded_at"`
	Encrypted  bool      `json:"encrypted"`
}

type Backup struct {
	Label       string    `json:"label"`
	Started     time.Time `json:"started"`
	Finished    time.Time `json:"finished"`
	SizeGB      float64   `json:"size_gb"`
	WALStartLSN string    `json:"wal_start_lsn"`
	WALStopLSN  string    `json:"wal_stop_lsn"`
	Status      string    `json:"status"`
}

type Snapshot struct {
	Receiver Receiver
	Status   *PgrwlStatus
	Config   *BriefConfig
	WALFiles []WALFile
	Backups  []Backup
	Error    string
}
