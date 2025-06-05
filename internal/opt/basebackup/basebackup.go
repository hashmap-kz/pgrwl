package basebackup

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// https://www.postgresql.org/docs/current/protocol-replication.html#PROTOCOL-REPLICATION-BASE-BACKUP

type BaseBackup interface {
	StreamBackup(ctx context.Context) error
}

type baseBackup struct {
	l       *slog.Logger
	conn    *pgconn.PgConn
	storage st.Storage
}

func NewBaseBackup(conn *pgconn.PgConn, storage st.Storage) (BaseBackup, error) {
	if conn == nil {
		return nil, fmt.Errorf("basebackup: connection is required")
	}
	if storage == nil {
		return nil, fmt.Errorf("basebackup: storage is required")
	}
	return &baseBackup{
		l:       slog.With("component", "basebackup"),
		conn:    conn,
		storage: storage,
	}, nil
}

func (bb *baseBackup) log() *slog.Logger {
	if bb.l != nil {
		return bb.l
	}
	return slog.With("component", "basebackup")
}

func (bb *baseBackup) StreamBackup(ctx context.Context) error {
	startResp, err := pglogrepl.StartBaseBackup(ctx, bb.conn, pglogrepl.BaseBackupOptions{
		Label:         fmt.Sprintf("pgrwl_%s", time.Now().UTC().Format("20060102150405")),
		Progress:      false,
		Fast:          true,
		WAL:           false,
		NoWait:        true,
		MaxRate:       0,
		TablespaceMap: true,
	})
	if err != nil {
		return fmt.Errorf("start base backup: %w", err)
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

	for {
		msg, err := bb.conn.ReceiveMessage(ctx)
		if err != nil {
			//nolint:errcheck
			_ = cleanup() // still try to cleanup
			return fmt.Errorf("receive message: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.CopyOutResponse:
			continue

		case *pgproto3.CopyData:
			switch m.Data[0] {
			case 'n':
				if err := cleanup(); err != nil {
					return err
				}

				buf := m.Data[1:]
				filename, rest, err := readCString(buf)
				if err != nil {
					return err
				}

				tsPath, _, err := readCString(rest)
				if err != nil {
					return err
				}

				remotePath := strings.TrimPrefix(filename, "./")
				pr, pw := io.Pipe()
				pipeWriter = pw
				putDone = make(chan struct{})

				go func(path string, r io.Reader) {
					defer close(putDone)
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
					return fmt.Errorf("received data but no active file")
				}
				if _, err := pipeWriter.Write(m.Data[1:]); err != nil {
					//nolint:errcheck
					_ = cleanup()
					return fmt.Errorf("write to pipe: %w", err)
				}

			case 'm':
				bb.log().Info("received manifest message type, ignored")

			case 'p':
				if len(m.Data) >= 9 {
					//nolint:gosec
					bytesDone := int64(binary.BigEndian.Uint64(m.Data[1:9]))
					bb.log().Info("progress",
						slog.Int64("bytes streamed from current file", bytesDone),
					)
				}

			default:
				bb.log().Warn("unknown CopyData type",
					slog.String("rune", string(m.Data[0])),
				)
			}

		case *pgproto3.CopyDone:
			if err := cleanup(); err != nil {
				return err
			}
			bb.log().Info("backup stream complete")

			stopRes, err := pglogrepl.FinishBaseBackup(ctx, bb.conn)
			if err != nil {
				return fmt.Errorf("finish base backup: %w", err)
			}

			bb.log().Info("finished backup", slog.String("LSN", stopRes.LSN.String()))
			return nil

		default:
			return fmt.Errorf("unexpected message type: %T", msg)
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
