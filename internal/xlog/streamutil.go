package xlog

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrSlotDoesNotExist = fmt.Errorf("replication slot does not exist")

// IdentifySystemResult is the parsed result of the IDENTIFY_SYSTEM command.
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
	var restartTLI int64

	if string(row[1]) != "" {
		restartLSN, err = pglogrepl.ParseLSN(string(row[1]))
		if err != nil {
			return isr, fmt.Errorf("failed to parse restart_lsn: %w", err)
		}
	}

	if string(row[2]) != "" {
		restartTLI, err = strconv.ParseInt(string(row[2]), 10, 32)
		if err != nil {
			return isr, fmt.Errorf("failed to parse restart_tli: %w", err)
		}
	}

	isr.SlotType = string(row[0])
	isr.RestartLSN = pglogrepl.LSN(restartLSN)
	isr.RestartTLI = uint32(restartTLI)
	return isr, nil
}
