package cmd

import (
	"context"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backupmode"

	"github.com/hashmap-kz/pgrwl/config"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return backupmode.RestoreBaseBackup(ctx, cfg, id, dest)
}
