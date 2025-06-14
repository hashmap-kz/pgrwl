package backup

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// https://www.postgresql.org/docs/current/protocol-replication.html#PROTOCOL-REPLICATION-BASE-BACKUP

// BaseBackup is an API for streaming basebackup
type BaseBackup interface {
	StreamBackup(ctx context.Context) (*Result, error)
}

// Tablespace represents a tablespace in the backup
type Tablespace struct {
	OID      int32  `json:"oid,omitempty"`
	Location string `json:"location,omitempty"`
	Size     int8   `json:"size,omitempty"`
}

// Result will hold the return values  of the BaseBackup command
type Result struct {
	LSN         pglogrepl.LSN `json:"lsn,omitempty"`
	TimelineID  int32         `json:"timeline_id,omitempty"`
	Tablespaces []Tablespace  `json:"tablespaces,omitempty"`
}

type baseBackup struct {
	l         *slog.Logger
	conn      *pgconn.PgConn
	storage   st.Storage
	timestamp string
}

func NewBaseBackup(conn *pgconn.PgConn, storage st.Storage, timestamp string) (BaseBackup, error) {
	if conn == nil {
		return nil, fmt.Errorf("basebackup: connection is required")
	}
	if storage == nil {
		return nil, fmt.Errorf("basebackup: storage is required")
	}
	if timestamp == "" {
		return nil, fmt.Errorf("basebackup: timestamp is required")
	}
	return &baseBackup{
		l:         slog.With(slog.String("component", "basebackup"), slog.String("id", timestamp)),
		conn:      conn,
		storage:   storage,
		timestamp: timestamp,
	}, nil
}

func (bb *baseBackup) log() *slog.Logger {
	if bb.l != nil {
		return bb.l
	}
	return slog.With(slog.String("component", "basebackup"), slog.String("id", bb.timestamp))
}

func (bb *baseBackup) StreamBackup(ctx context.Context) (*Result, error) {
	result, err := bb.streamBaseBackup(ctx)
	if err != nil {
		return nil, err
	}
	// upload marker
	markerFileName := bb.timestamp + ".json"
	bb.log().Debug("uploading marker file", slog.String("name", markerFileName))
	markerFileData, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	err = bb.storage.Put(ctx, markerFileName, io.NopCloser(bytes.NewReader(markerFileData)))
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (bb *baseBackup) streamBaseBackup(ctx context.Context) (*Result, error) {
	startResp, err := pglogrepl.StartBaseBackup(ctx, bb.conn, pglogrepl.BaseBackupOptions{
		Label:         fmt.Sprintf("pgrwl_%s", bb.timestamp),
		Progress:      false,
		Fast:          true,
		WAL:           false,
		NoWait:        true,
		MaxRate:       0,
		TablespaceMap: true,
	})
	if err != nil {
		return nil, fmt.Errorf("start base backup: %w", err)
	}

	bb.log().Info("started backup",
		slog.String("LSN", startResp.LSN.String()),
	)

	var pipeWriter *io.PipeWriter
	var putErr error
	var putDone chan struct{}

	cleanup := func() error {
		if pipeWriter != nil {
			_ = pipeWriter.Close()
			<-putDone
			if putErr != nil {
				return fmt.Errorf("storage put failed: %w", putErr)
			}
		}
		return nil
	}

	var remotePath string

	for {
		msg, err := bb.conn.ReceiveMessage(ctx)
		if err != nil {
			//nolint:errcheck
			_ = cleanup() // still try to cleanup
			return nil, fmt.Errorf("receive message: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.CopyOutResponse:
			continue

		case *pgproto3.CopyData:
			switch m.Data[0] {
			case 'n':
				if err := cleanup(); err != nil {
					return nil, err
				}

				buf := m.Data[1:]
				filename, rest, err := readCString(buf)
				if err != nil {
					return nil, err
				}

				tsPath, _, err := readCString(rest)
				if err != nil {
					return nil, err
				}

				remotePath = strings.TrimPrefix(filename, "./")
				pr, pw := io.Pipe()
				pipeWriter = pw
				putDone = make(chan struct{})

				go func(path string, r io.Reader) {
					defer func() {
						bb.log().Info("closing", slog.String("file", path))
						close(putDone)
					}()
					putErr = bb.storage.Put(ctx, path, r)
				}(remotePath, pr)

				bb.log().Info("streaming file",
					slog.String("path", remotePath),
					slog.String("tablespace-path", tsPath),
				)

			case 'd':
				if pipeWriter == nil {
					//nolint:errcheck
					_ = cleanup()
					return nil, fmt.Errorf("received data but no active file")
				}
				if _, err := pipeWriter.Write(m.Data[1:]); err != nil {
					//nolint:errcheck
					_ = cleanup()
					return nil, fmt.Errorf("write to pipe: %w", err)
				}

			case 'm':
				bb.log().Info("received manifest message type, ignored")

			case 'p':
				if len(m.Data) >= 9 {
					//nolint:gosec
					bytesDone := int64(binary.BigEndian.Uint64(m.Data[1:9]))
					bb.log().Info("progress",
						slog.String("file", remotePath),
						slog.Int64("bytes streamed", bytesDone),
						slog.String("bytes streamed IEC", optutils.ByteCountIEC(bytesDone)),
					)
				}

			default:
				bb.log().Warn("unknown CopyData type",
					slog.String("rune", string(m.Data[0])),
				)
			}

		case *pgproto3.CopyDone:
			if err := cleanup(); err != nil {
				return nil, err
			}
			bb.log().Info("backup stream complete")

			stopRes, err := pglogrepl.FinishBaseBackup(ctx, bb.conn)
			if err != nil {
				return nil, fmt.Errorf("finish base backup: %w", err)
			}

			bb.log().Info("finished backup", slog.String("LSN", stopRes.LSN.String()))
			var tablespaces []Tablespace
			for _, ts := range stopRes.Tablespaces {
				tablespaces = append(tablespaces, Tablespace{
					OID:      ts.OID,
					Location: ts.Location,
					Size:     ts.Size,
				})
			}
			return &Result{
				LSN:         stopRes.LSN,
				TimelineID:  stopRes.TimelineID,
				Tablespaces: tablespaces,
			}, nil

		default:
			return nil, fmt.Errorf("unexpected message type: %T", msg)
		}
	}
}

//nolint:gocritic
func readCString(buf []byte) (string, []byte, error) {
	idx := bytes.IndexByte(buf, 0)
	if idx < 0 {
		return "", nil, fmt.Errorf("invalid CString: %q", string(buf))
	}
	return string(buf[:idx]), buf[idx+1:], nil
}
