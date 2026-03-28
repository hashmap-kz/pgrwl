package cmd

import (
	"context"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/modes/restorecmd"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	return restorecmd.RestoreBaseBackup(ctx, cfg, id, dest)
}
