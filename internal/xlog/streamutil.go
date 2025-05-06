package xlog

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/hashmap-kz/pgreceivewal/internal/conv"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrSlotDoesNotExist = fmt.Errorf("replication slot does not exist")

// ReadReplicationSlotResultResult is the parsed result of the READ_REPLICATION_SLOT command.
type ReadReplicationSlotResultResult struct {
	// https://www.postgresql.org/docs/current/protocol-replication.html#PROTOCOL-REPLICATION-READ-REPLICATION-SLOT
	//
	//	slot_type (text)
	//	The replication slot's type, either physical or NULL.
	//
	//	restart_lsn (text)
	//	The replication slot's restart_lsn.
	//
	//	restart_tli (int8)

	SlotType   string
	RestartLSN pglogrepl.LSN
	RestartTLI uint32
}

func GetSlotInformation(conn *pgconn.PgConn, slotName string) (*ReadReplicationSlotResultResult, error) {
	res := conn.Exec(context.Background(), "READ_REPLICATION_SLOT "+slotName)
	rs, err := parseReadReplicationSlot(res)
	if err != nil {
		return nil, err
	}
	return &rs, nil
}

// parseReadReplicationSlot parses the result of the READ_REPLICATION_SLOT command.
func parseReadReplicationSlot(mrr *pgconn.MultiResultReader) (ReadReplicationSlotResultResult, error) {
	var isr ReadReplicationSlotResultResult
	results, err := mrr.ReadAll()
	if err != nil {
		return isr, err
	}

	if len(results) != 1 {
		return isr, fmt.Errorf("expected 1 result set, got %d", len(results))
	}

	result := results[0]
	if len(result.Rows) != 1 {
		return isr, fmt.Errorf("expected 1 result row, got %d", len(result.Rows))
	}

	row := result.Rows[0]
	if len(row) != 3 {
		return isr, fmt.Errorf("expected 3 result columns, got %d", len(row))
	}

	if len(row[0]) == 0 {
		return isr, ErrSlotDoesNotExist
	}

	var restartLSN pglogrepl.LSN
	var restartTLI uint32

	if len(row[1]) != 0 {
		restartLSN, err = pglogrepl.ParseLSN(string(row[1]))
		if err != nil {
			return isr, fmt.Errorf("failed to parse restart_lsn: %w", err)
		}
	}

	if len(row[2]) != 0 {
		restartTLI, err = conv.ParseUint32(string(row[2]))
		if err != nil {
			return isr, fmt.Errorf("failed to parse restart_tli: %w", err)
		}
	}

	isr.SlotType = string(row[0])
	isr.RestartLSN = restartLSN
	isr.RestartTLI = restartTLI
	return isr, nil
}

// parameters

// GetShowParameter executes "SHOW parameterName" and returns its string value.
func GetShowParameter(conn *pgconn.PgConn, parameterName string) (string, error) {
	query := "SHOW " + parameterName

	mrr := conn.Exec(context.Background(), query)

	value, err := parseShowParameter(mrr)
	if err != nil {
		return "", fmt.Errorf("failed to read SHOW %s: %w", parameterName, err)
	}

	return value, nil
}

// parseShowParameter parses the result of the SHOW command.
func parseShowParameter(mrr *pgconn.MultiResultReader) (string, error) {
	results, err := mrr.ReadAll()
	if err != nil {
		return "", err
	}

	if len(results) != 1 {
		return "", fmt.Errorf("expected 1 result set, got %d", len(results))
	}

	result := results[0]
	if len(result.Rows) != 1 {
		return "", fmt.Errorf("expected 1 result row, got %d", len(result.Rows))
	}

	row := result.Rows[0]
	if len(row) < 1 {
		return "", fmt.Errorf("expected at least 1 column in SHOW result, got %d", len(row))
	}

	return string(row[0]), nil
}

// scan utils

// ScanWalSegSize scans wal segment size with units, e.g. "16MB" into bytes size
func ScanWalSegSize(sizeStr string) (uint64, error) {
	if sizeStr == "" {
		return 0, fmt.Errorf("empty input")
	}

	var val int
	var unit string
	_, err := fmt.Sscanf(sizeStr, "%d%2s", &val, &unit)
	if err != nil {
		return 0, err
	}

	if val == 0 {
		return 0, fmt.Errorf("segment size cannot be zero")
	}
	if unit == "" {
		return 0, fmt.Errorf("unit cannot be empty")
	}

	var multiplier int
	switch unit {
	case "MB":
		multiplier = 1024 * 1024
	case "GB":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}

	segSize := conv.ToUint64(int64(val * multiplier))

	if !IsValidWalSegSize(segSize) {
		return 0, fmt.Errorf("wal_segment_size is out of range: %d", val)
	}

	return segSize, nil
}

// startup info

type StartupInfo struct {
	WalSegSz uint64
}

func GetStartupInfo(conn *pgconn.PgConn) (*StartupInfo, error) {
	// WAL segment size
	p, err := GetShowParameter(conn, "wal_segment_size")
	if err != nil {
		return nil, err
	}
	wss, err := ScanWalSegSize(p)
	if err != nil {
		return nil, err
	}

	// Server version
	p, err = GetShowParameter(conn, "server_version_num")
	if err != nil {
		return nil, err
	}
	serverVersion, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		return nil, err
	}
	if serverVersion < 150000 {
		// because 'READ_REPLICATION_SLOT' that we use was introduced in v15
		// anyway, this particular error may be revived later
		return nil, fmt.Errorf("unsupported version %d, postgresql >=15 is expected", serverVersion)
	}

	return &StartupInfo{
		WalSegSz: wss,
	}, nil
}

// modified version from pglogrepl
// perhaps will be fixed: https://github.com/jackc/pglogrepl/pull/77

func SendStandbyCopyDone(_ context.Context, conn *pgconn.PgConn) (cdr *pglogrepl.CopyDoneResult, err error) {
	cdr = &pglogrepl.CopyDoneResult{} // <<< Fix: initialize the pointer!

	conn.Frontend().Send(&pgproto3.CopyDone{})
	err = conn.Frontend().Flush()
	if err != nil {
		return cdr, err
	}

	for {
		var msg pgproto3.BackendMessage
		msg, err = conn.Frontend().Receive()
		if err != nil {
			return cdr, err
		}

		switch m := msg.(type) {
		case *pgproto3.CopyDone:
			// ignore
		case *pgproto3.ParameterStatus, *pgproto3.NoticeResponse:
			// ignore
		case *pgproto3.CommandComplete:
			// ignore
		case *pgproto3.RowDescription:
			// ignore
		case *pgproto3.DataRow:
			if len(m.Values) == 2 {
				timeline, lerr := strconv.Atoi(string(m.Values[0]))
				if lerr == nil {
					lsn, lerr := pglogrepl.ParseLSN(string(m.Values[1]))
					if lerr == nil {
						cdr.Timeline = int32(timeline) //nolint:gosec
						cdr.LSN = lsn
					}
				}
			}
		case *pgproto3.EmptyQueryResponse:
			// ignore
		case *pgproto3.ErrorResponse:
			return cdr, pgconn.ErrorResponseToPgError(m)
		case *pgproto3.ReadyForQuery:
			return cdr, err
		}
	}
}
