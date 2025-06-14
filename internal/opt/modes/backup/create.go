package backup

import (
	"context"
	"log/slog"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/opt/compn"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/jackc/pgx/v5/pgconn"
)

type CreateBaseBackupOpts struct {
	Directory string
}

func CreateBaseBackup(opts *CreateBaseBackupOpts) error {
	var err error

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// timestamp
	ts := time.Now().UTC().Format("20060102150405")
	loggr := slog.With(slog.String("component", "basebackup"), slog.String("id", ts))

	// setup storage
	stor, err := compn.SetupStorage(&compn.SetupStorageOpts{
		BaseDir: opts.Directory,
		SubPath: filepath.ToSlash(filepath.Join(config.BaseBackupSubpath, ts)),
	})
	if err != nil {
		loggr.Error("cannot init storage", slog.Any("err", err))
		return err
	}

	// create connection
	conn, err := pgconn.Connect(ctx, "application_name=pgrwl_basebackup replication=yes")
	if err != nil {
		loggr.Error("cannot establish connection", slog.Any("err", err))
		return err
	}

	// init module
	baseBackup, err := NewBaseBackup(conn, stor, ts)
	if err != nil {
		loggr.Error("cannot init basebackup module", slog.Any("err", err))
		return err
	}

	// stream basebackup to defined storage
	_, err = baseBackup.StreamBackup(ctx)
	if err != nil {
		loggr.Error("cannot create basebackup", slog.Any("err", err))
		return err
	}

	loggr.Info("basebackup successfully created")
	return nil
}
