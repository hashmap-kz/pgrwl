package pg

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
)

const (
	DefaultConnWaitTimeout       = 30 * time.Second
	DefaultConnSleepWhileWaiting = 5 * time.Second
)

type QuerySession interface {
	SystemIdentifier() int64
	ServerVersion() int
	GetParameter(name string) (string, error)
	WalSegmentSize() uint64
	DataDirectory() string
	SlotInfo(slotName string) (PhysicalSlot, error)
	Close() error
}

type querySession struct {
	connStr          string
	conn             *pgx.Conn
	serverVersion    int
	systemIdentifier int64
	dataDirectory    string
	walSegmentSize   uint64
}

type PhysicalSlot struct {
	Name       string
	Exists     bool
	Active     bool
	RestartLSN pglogrepl.LSN
}

var _ QuerySession = &querySession{}

// xlog_internal.c

const (
	WalSegMinSize = 1 * 1024 * 1024        // 1 MiB
	WalSegMaxSize = 1 * 1024 * 1024 * 1024 // 1 GiB
)

// isPowerOf2 returns true if x is a power of 2
func isPowerOf2(x uint64) bool {
	return x > 0 && (x&(x-1)) == 0
}

// isValidWalSegSize checks if size is a valid wal_segment_size (1MiB..1GiB and power of 2)
func isValidWalSegSize(size uint64) bool {
	return isPowerOf2(size) && (size >= WalSegMinSize && size <= WalSegMaxSize)
}

func ValidateConnStr(connStr string) error {
	deadline := time.Now().Add(DefaultConnWaitTimeout)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		conn, err := pgx.Connect(ctx, connStr)
		if err == nil {
			if err := conn.Ping(ctx); err != nil {
				conn.Close(ctx)
				return fmt.Errorf("ping failed: %w", err)
			}
			slog.Info("pg_isready", slog.String("status", "ok"))
			conn.Close(ctx)
			return nil // Ready
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("PostgreSQL not ready after %s: %w", DefaultConnWaitTimeout, err)
		}

		slog.Info("pg_isready", slog.String("status", "waiting"))
		time.Sleep(DefaultConnSleepWhileWaiting)
	}
}

func NewPGQuerySession(ctx context.Context, connStr string) (QuerySession, error) {
	err := ValidateConnStr(connStr)
	if err != nil {
		return nil, err
	}

	connection, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, err
	}

	query := `
	select (current_setting('server_version_num'))::int                                    as server_version_num,
		   (select system_identifier from pg_catalog.pg_control_system())::bigint            as system_identifier,
		   (select setting from pg_catalog.pg_settings where name = 'data_directory')::text  as data_directory,
		   (select setting from pg_catalog.pg_settings where name = 'wal_segment_size')::int as wal_segment_size
	`

	backupSession := querySession{
		connStr: connStr,
		conn:    connection,
	}
	err = connection.QueryRow(context.TODO(), query).
		Scan(
			&backupSession.serverVersion,
			&backupSession.systemIdentifier,
			&backupSession.dataDirectory,
			&backupSession.walSegmentSize,
		)
	if err != nil {
		return nil, err
	}

	if backupSession.serverVersion < 150000 {
		return nil, fmt.Errorf("unsupported version %d, postgresql >=15 is expected", backupSession.serverVersion)
	}

	if !isValidWalSegSize(backupSession.walSegmentSize) {
		return nil, fmt.Errorf("walSegmentSize is out of range: %d", backupSession.walSegmentSize)
	}

	return &backupSession, nil
}

func (qs *querySession) GetParameter(name string) (string, error) {
	var value string
	err := qs.conn.QueryRow(context.TODO(), "select setting from pg_catalog.pg_settings where name = $1", name).Scan(&value)
	return value, err
}

func (qs *querySession) SystemIdentifier() int64 {
	return qs.systemIdentifier
}

func (qs *querySession) ServerVersion() int {
	return qs.serverVersion
}

func (qs *querySession) WalSegmentSize() uint64 {
	return qs.walSegmentSize
}

func (qs *querySession) DataDirectory() string {
	return qs.dataDirectory
}

func (qs *querySession) SlotInfo(slotName string) (PhysicalSlot, error) {
	query := `
	select
		active,
		coalesce(restart_lsn, '0/0'::pg_lsn)
	from pg_catalog.pg_replication_slots
	where slot_name = $1
	`

	var active bool
	var restartLSN string
	err := qs.conn.QueryRow(context.TODO(), query, slotName).Scan(&active, &restartLSN)
	if err == pgx.ErrNoRows {
		return PhysicalSlot{Name: slotName}, nil
	} else if err != nil {
		return PhysicalSlot{Name: slotName}, err
	}

	restLSN, err := pglogrepl.ParseLSN(restartLSN)
	if err != nil {
		return PhysicalSlot{
			Name:   slotName,
			Exists: true,
			Active: active,
		}, err
	}

	return PhysicalSlot{
		Name:       slotName,
		Exists:     true,
		Active:     active,
		RestartLSN: restLSN,
	}, nil
}

func (qs *querySession) Close() error {
	if qs.conn != nil {
		return qs.conn.Close(context.TODO())
	}
	return nil
}
