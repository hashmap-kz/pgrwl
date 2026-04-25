package backupdto

import (
	"time"

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
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	Manifest    *BackupManifest `json:"manifest,omitempty"`
}

// PostgreSQL manifest

// {
//     "PostgreSQL-Backup-Manifest-Version": 1,
//     "Files": [
//         {
//             "Path": "pg_tblspc/108562/PG_16_202307071/106992/107015",
//             "Size": 0,
//             "Last-Modified": "2025-11-28 06:19:09 GMT",
//             "Checksum-Algorithm": "CRC32C",
//             "Checksum": "00000000"
//         },
//         {
//             "Path": "pg_tblspc/108562/PG_16_202307071/106992/107016",
//             "Size": 8192,
//             "Last-Modified": "2025-11-28 06:19:09 GMT",
//             "Checksum-Algorithm": "CRC32C",
//             "Checksum": "8ee8be27"
//         }
//     ],
//     "WAL-Ranges": [
//         {
//             "Timeline": 1,
//             "Start-LSN": "12/48000028",
//             "End-LSN": "12/48000138"
//         }
//     ],
//     "Manifest-Checksum": "12c7b73706ba9c82429483ad1f97c394de347cb86b7ad1e28377012228389d63"
// }

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
