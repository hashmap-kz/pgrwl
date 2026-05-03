package backupsv

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pgrwl/pgrwl/config"
)

type RetentionService interface {
	RunBeforeBackup(ctx context.Context) error
}

type NoopRetention struct{}

func (NoopRetention) RunBeforeBackup(ctx context.Context) error {
	return ctx.Err()
}

func NewRetentionService(opts *BackupSupervisorOpts) RetentionService {
	if opts.Cfg == nil || !opts.Cfg.Retention.Enable {
		return NoopRetention{}
	}

	return &ConfiguredRetention{
		l:    slog.With(slog.String("component", "basebackup-retention")),
		opts: opts,
	}
}

type ConfiguredRetention struct {
	l    *slog.Logger
	opts *BackupSupervisorOpts
}

func (r *ConfiguredRetention) RunBeforeBackup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	cfg := r.opts.Cfg
	if !cfg.Retention.Enable {
		return nil
	}

	switch cfg.Retention.Type {
	case config.RetentionTypeRecoveryWindow:
		retention := NewRecoveryWindowRetention(r.opts)
		return retention.RunBeforeBackup(ctx)

	default:
		return fmt.Errorf(
			"unsupported backup retention type %q: only %q is supported",
			cfg.Retention.Type,
			config.RetentionTypeRecoveryWindow,
		)
	}
}
