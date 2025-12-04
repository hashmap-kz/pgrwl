package backupdto

import "github.com/jackc/pglogrepl"

// https://www.postgresql.org/docs/current/protocol-replication.html#PROTOCOL-REPLICATION-BASE-BACKUP

// Tablespace represents a tablespace in the backup
type Tablespace struct {
	OID      int32  `json:"oid,omitempty"`
	Location string `json:"location,omitempty"`
}

// Result will hold the return values  of the BaseBackup command
type Result struct {
	StartLSN    pglogrepl.LSN `json:"start_lsn,omitempty"`
	StopLSN     pglogrepl.LSN `json:"stop_lsn,omitempty"`
	TimelineID  int32         `json:"timeline_id,omitempty"`
	Tablespaces []Tablespace  `json:"tablespaces,omitempty"`
	BytesTotal  int64         `json:"bytes_total,omitempty"`
}
