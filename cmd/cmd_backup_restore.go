package cmd

import (
	"context"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/restore"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return restore.RestoreBaseBackup(ctx, cfg, id, dest)
}
