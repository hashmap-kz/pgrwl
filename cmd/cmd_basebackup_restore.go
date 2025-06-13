package cmd

import (
	"context"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/basebackup"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return basebackup.RestoreBaseBackup(ctx, cfg, id, dest)
}
