package backupmode

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

type BaseBackupOptions struct {
	// Request information required to generate a progress report, but might as such have a negative impact on the performance.
	Progress bool
	// Sets the label of the backup. If none is specified, a backup label of 'wal-g' will be used.
	Label string
	// Request a fast checkpoint.
	Fast bool
	// Include the necessary WAL segments in the backup. This will include all the files between start and stop backup in the pg_wal directory of the base directory tar file.
	WAL bool
	// By default, the backup will wait until the last required WAL segment has been archived, or emit a warning if log archiving is not enabled.
	// Specifying NOWAIT disables both the waiting and the warning, leaving the client responsible for ensuring the required log is available.
	NoWait bool
	// Limit (throttle) the maximum amount of data transferred from server to client per unit of time (kb/s).
	MaxRate int32
	// Include information about symbolic links present in the directory pg_tblspc in a file named tablespace_map.
	TablespaceMap bool
	// Disable checksums being verified during a base backup.
	// Note that NoVerifyChecksums=true is only supported since PG11
	NoVerifyChecksums bool

	// When this option is specified with a value of yes or force-encode,
	// a backup manifest is created and sent along with the backup.
	// The manifest is a list of every file present in the backup with the exception of any WAL files that may be included.
	// It also stores the size, last modification time, and optionally a checksum for each file.
	// A value of force-encode forces all filenames to be hex-encoded; otherwise,
	// this type of encoding is performed only for files whose names are non-UTF8 octet sequences.
	// force-encode is intended primarily for testing purposes, to be sure that clients which read
	// the backup manifest can handle this case.
	// For compatibility with previous releases, the default is MANIFEST 'no'.
	Manifest string

	// Specifies the checksum algorithm that should be applied to each file included in the backup manifest.
	// Currently, the available algorithms are NONE, CRC32C, SHA224, SHA256, SHA384, and SHA512. The default is CRC32C.
	ManifestChecksums string
}

func (bbo BaseBackupOptions) sql(serverVersion int) string {
	var parts []string
	if bbo.Label != "" {
		parts = append(parts, "LABEL '"+strings.ReplaceAll(bbo.Label, "'", "''")+"'")
	}
	if bbo.Progress {
		parts = append(parts, "PROGRESS")
	}
	if bbo.Fast {
		if serverVersion >= 15 {
			parts = append(parts, "CHECKPOINT 'fast'")
		} else {
			parts = append(parts, "FAST")
		}
	}
	if bbo.WAL {
		parts = append(parts, "WAL")
	}
	if bbo.NoWait {
		if serverVersion >= 15 {
			parts = append(parts, "WAIT false")
		} else {
			parts = append(parts, "NOWAIT")
		}
	}
	if bbo.MaxRate >= 32 {
		parts = append(parts, fmt.Sprintf("MAX_RATE %d", bbo.MaxRate))
	}
	if bbo.TablespaceMap {
		parts = append(parts, "TABLESPACE_MAP")
	}
	if bbo.NoVerifyChecksums {
		if serverVersion >= 15 {
			parts = append(parts, "VERIFY_CHECKSUMS false")
		} else if serverVersion >= 11 {
			parts = append(parts, "NOVERIFY_CHECKSUMS")
		}
	}
	if bbo.Manifest != "" {
		if bbo.Manifest == "yes" {
			parts = append(parts, "MANIFEST yes")
		}
	}
	if serverVersion >= 15 {
		return "BASE_BACKUP(" + strings.Join(parts, ", ") + ")"
	}
	return "BASE_BACKUP " + strings.Join(parts, " ")
}

// BaseBackupTablespace represents a tablespace in the backup
type BaseBackupTablespace struct {
	OID      int32
	Location string
	Size     int8
}

// BaseBackupResult will hold the return values  of the BaseBackup command
type BaseBackupResult struct {
	LSN         pglogrepl.LSN
	TimelineID  int32
	Tablespaces []BaseBackupTablespace
}

func serverMajorVersion(conn *pgconn.PgConn) (int, error) {
	verString := conn.ParameterStatus("server_version")
	dot := strings.IndexByte(verString, '.')
	if dot == -1 {
		return 0, fmt.Errorf("bad server version string: '%s'", verString)
	}
	return strconv.Atoi(verString[:dot])
}

// StartBaseBackup begins the process for copying a basebackup by executing the BASE_BACKUP command.
func StartBaseBackup(ctx context.Context, conn *pgconn.PgConn, options BaseBackupOptions) (result BaseBackupResult, err error) {
	serverVersion, err := serverMajorVersion(conn)
	if err != nil {
		return result, err
	}
	sql := options.sql(serverVersion)

	conn.Frontend().SendQuery(&pgproto3.Query{String: sql})
	err = conn.Frontend().Flush()
	if err != nil {
		return result, fmt.Errorf("failed to send BASE_BACKUP: %w", err)
	}
	// From here Postgres returns result sets, but pgconn has no infrastructure to properly capture them.
	// So we capture data low level with sub functions, before we return from this function when we get to the CopyData part.
	result.LSN, result.TimelineID, err = getBaseBackupInfo(ctx, conn)
	if err != nil {
		return result, err
	}
	result.Tablespaces, err = getTableSpaceInfo(ctx, conn)
	return result, err
}

// getBaseBackupInfo returns the start or end position of the backup as returned by Postgres
func getBaseBackupInfo(ctx context.Context, conn *pgconn.PgConn) (start pglogrepl.LSN, timelineID int32, err error) {
	for {
		msg, err := conn.ReceiveMessage(ctx)
		if err != nil {
			return start, timelineID, fmt.Errorf("failed to receive message: %w", err)
		}
		switch msg := msg.(type) {
		case *pgproto3.RowDescription:
			if len(msg.Fields) != 2 {
				return start, timelineID, fmt.Errorf("expected 2 column headers, received: %d", len(msg.Fields))
			}
			colName := string(msg.Fields[0].Name)
			if colName != "recptr" {
				return start, timelineID, fmt.Errorf("unexpected col name for recptr col: %s", colName)
			}
			colName = string(msg.Fields[1].Name)
			if colName != "tli" {
				return start, timelineID, fmt.Errorf("unexpected col name for tli col: %s", colName)
			}
		case *pgproto3.DataRow:
			if len(msg.Values) != 2 {
				return start, timelineID, fmt.Errorf("expected 2 columns, received: %d", len(msg.Values))
			}
			colData := string(msg.Values[0])
			start, err = pglogrepl.ParseLSN(colData)
			if err != nil {
				return start, timelineID, fmt.Errorf("cannot convert result to LSN: %s", colData)
			}
			colData = string(msg.Values[1])
			tli, err := strconv.Atoi(colData)
			if err != nil {
				return start, timelineID, fmt.Errorf("cannot convert timelineID to int: %s", colData)
			}
			timelineID = int32(tli)
		case *pgproto3.NoticeResponse:
		case *pgproto3.CommandComplete:
			return start, timelineID, nil
		case *pgproto3.ErrorResponse:
			return start, timelineID, fmt.Errorf("error response sev=%q code=%q message=%q detail=%q position=%d", msg.Severity, msg.Code, msg.Message, msg.Detail, msg.Position)
		default:
			return start, timelineID, fmt.Errorf("unexpected response type: %T", msg)
		}
	}
}

// getBaseBackupInfo returns the start or end position of the backup as returned by Postgres
func getTableSpaceInfo(ctx context.Context, conn *pgconn.PgConn) (tbss []BaseBackupTablespace, err error) {
	for {
		msg, err := conn.ReceiveMessage(ctx)
		if err != nil {
			return tbss, fmt.Errorf("failed to receive message: %w", err)
		}
		switch msg := msg.(type) {
		case *pgproto3.RowDescription:
			if len(msg.Fields) != 3 {
				return tbss, fmt.Errorf("expected 3 column headers, received: %d", len(msg.Fields))
			}
			colName := string(msg.Fields[0].Name)
			if colName != "spcoid" {
				return tbss, fmt.Errorf("unexpected col name for spcoid col: %s", colName)
			}
			colName = string(msg.Fields[1].Name)
			if colName != "spclocation" {
				return tbss, fmt.Errorf("unexpected col name for spclocation col: %s", colName)
			}
			colName = string(msg.Fields[2].Name)
			if colName != "size" {
				return tbss, fmt.Errorf("unexpected col name for size col: %s", colName)
			}
		case *pgproto3.DataRow:
			if len(msg.Values) != 3 {
				return tbss, fmt.Errorf("expected 3 columns, received: %d", len(msg.Values))
			}
			if msg.Values[0] == nil {
				continue
			}
			tbs := BaseBackupTablespace{}
			colData := string(msg.Values[0])
			OID, err := strconv.Atoi(colData)
			if err != nil {
				return tbss, fmt.Errorf("cannot convert spcoid to int: %s", colData)
			}
			tbs.OID = int32(OID)
			tbs.Location = string(msg.Values[1])
			if msg.Values[2] != nil {
				colData := string(msg.Values[2])
				size, err := strconv.Atoi(colData)
				if err != nil {
					return tbss, fmt.Errorf("cannot convert size to int: %s", colData)
				}
				tbs.Size = int8(size)
			}
			tbss = append(tbss, tbs)
		case *pgproto3.CommandComplete:
			return tbss, nil
		default:
			return tbss, fmt.Errorf("unexpected response type: %T", msg)
		}
	}
}

// NextTableSpace consumes some msgs so we are at start of CopyData
func NextTableSpace(ctx context.Context, conn *pgconn.PgConn) (err error) {
	for {
		msg, err := conn.ReceiveMessage(ctx)
		if err != nil {
			return fmt.Errorf("failed to receive message: %w", err)
		}

		switch msg := msg.(type) {
		case *pgproto3.CopyOutResponse:
			return nil
		case *pgproto3.CopyData:
			return nil
		case *pgproto3.ErrorResponse:
			return pgconn.ErrorResponseToPgError(msg)
		case *pgproto3.NoticeResponse:
		case *pgproto3.RowDescription:

		default:
			return fmt.Errorf("unexpected response type: %T", msg)
		}
	}
}

// FinishBaseBackup wraps up a backup after copying all results from the BASE_BACKUP command.
func FinishBaseBackup(ctx context.Context, conn *pgconn.PgConn) (result BaseBackupResult, err error) {
	// From here Postgres returns result sets, but pgconn has no infrastructure to properly capture them.
	// So we capture data low level with sub functions, before we return from this function when we get to the CopyData part.
	result.LSN, result.TimelineID, err = getBaseBackupInfo(ctx, conn)
	if err != nil {
		return result, err
	}

	// Base_Backup done, server sends a command complete response
	var (
		pack pgproto3.BackendMessage
		ok   bool
	)
	pack, err = conn.ReceiveMessage(ctx)
	if err != nil {
		return
	}
	_, ok = pack.(*pgproto3.CommandComplete)
	if !ok {
		err = fmt.Errorf("expect command_complete, got %T", pack)
		return
	}

	// simple query done, server send a ready for query response
	pack, err = conn.ReceiveMessage(ctx)
	if err != nil {
		return
	}
	_, ok = pack.(*pgproto3.ReadyForQuery)
	if !ok {
		err = fmt.Errorf("expect ready_for_query, got %T", pack)
		return
	}
	return
}
