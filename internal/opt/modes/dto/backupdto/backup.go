package backupdto

import (
	"github.com/jackc/pglogrepl"
)

// https://www.postgresql.org/docs/current/protocol-replication.html#PROTOCOL-REPLICATION-BASE-BACKUP

// Tablespace represents a tablespace in the backup
type Tablespace struct {
	OID      int32  `json:"oid,omitempty"`
	Location string `json:"location,omitempty"`
}

// Result will hold the return values  of the BaseBackup command
type Result struct {
	StartLSN    pglogrepl.LSN   `json:"start_lsn,omitempty"`
	StopLSN     pglogrepl.LSN   `json:"stop_lsn,omitempty"`
	TimelineID  int32           `json:"timeline_id,omitempty"`
	Tablespaces []Tablespace    `json:"tablespaces,omitempty"`
	BytesTotal  int64           `json:"bytes_total,omitempty"`
	Manifest    *BackupManifest `json:"manifest,omitempty"`
}

// PostgreSQL manifest

// BackupManifest represents PostgreSQL backup manifest
type BackupManifest struct {
	Version          int                `json:"PostgreSQL-Backup-Manifest-Version"`
	Files            []ManifestFile     `json:"Files"`
	WALRanges        []ManifestWALRange `json:"WAL-Ranges"`
	ManifestChecksum string             `json:"Manifest-Checksum"`
}

// ManifestFile represents a single file entry in manifest
type ManifestFile struct {
	Path              string `json:"Path"`
	Size              int64  `json:"Size"`
	LastModified      string `json:"Last-Modified"`
	ChecksumAlgorithm string `json:"Checksum-Algorithm"`
	Checksum          string `json:"Checksum"`
}

// ManifestWALRange represents WAL coverage
type ManifestWALRange struct {
	Timeline int32  `json:"Timeline"`
	StartLSN string `json:"Start-LSN"`
	EndLSN   string `json:"End-LSN"`
}
