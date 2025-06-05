package cmd

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/basebackup"
	"github.com/hashmap-kz/pgrwl/internal/opt/supervisor"
	"github.com/jackc/pgx/v5/pgconn"
)

type BaseBackupCmdOpts struct {
	Directory string
}

func RunBaseBackup(opts *BaseBackupCmdOpts) error {
	var err error
	loggr := slog.With("component", "basebackup")

	// setup context
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// setup storage
	stor, err := supervisor.SetupStorage(&supervisor.SetupStorageOpts{
		BaseDir: opts.Directory,
		SubPath: config.BaseBackupSubpath,
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
	baseBackup, err := basebackup.NewBaseBackup(conn, stor)
	if err != nil {
		loggr.Error("cannot init basebackup module", slog.Any("err", err))
		return err
	}

	// stream basebackup to defined storage
	err = baseBackup.StreamBackup(ctx)
	if err != nil {
		loggr.Error("cannot create basebackup", slog.Any("err", err))
		return err
	}

	loggr.Info("basebackup successfully created")
	return nil
}
