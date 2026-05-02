package backupsv

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/shared/retry"
)

func LoadStartupInfo(ctx context.Context, l *slog.Logger) (*xlog.StartupInfo, error) {
	if l == nil {
		l = slog.With(slog.String("component", "basebackup-startup"))
	}

	conn, err := retry.Do(ctx, retry.Policy{
		Delay: 5 * time.Second,
		Logger: l.With(
			slog.String("retry-operation", "connect-replication"),
		),
	}, func(ctx context.Context) (*pgconn.PgConn, error) {
		return pgconn.Connect(ctx, "application_name=pgrwl_basebackup replication=yes")
	})
	if err != nil {
		return nil, fmt.Errorf("connect replication: %w", err)
	}
	defer func() {
		_ = conn.Close(context.Background())
	}()

	startupInfo, err := xlog.GetStartupInfo(conn)
	if err != nil {
		return nil, fmt.Errorf("get startup info: %w", err)
	}

	return startupInfo, nil
}
