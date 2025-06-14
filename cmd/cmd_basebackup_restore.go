package cmd

import (
	"context"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/backup"

	"github.com/hashmap-kz/pgrwl/config"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return backup.RestoreBaseBackup(ctx, cfg, id, dest)
}
