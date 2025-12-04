package cmd

import (
	"context"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/modes/restorecmd"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return restorecmd.RestoreBaseBackup(ctx, cfg, id, dest)
}
