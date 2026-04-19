package cmd

import (
	"context"

	"github.com/pgrwl/pgrwl/internal/opt/basebackup"

	"github.com/pgrwl/pgrwl/config"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return basebackup.RestoreBaseBackup(ctx, cfg, id, dest)
}
