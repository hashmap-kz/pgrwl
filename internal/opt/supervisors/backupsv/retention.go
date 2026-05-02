package backupsv

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type RetentionService interface {
	RunBeforeBackup(ctx context.Context, startupInfo *xlog.StartupInfo) error
}

type NoopRetention struct{}

func (NoopRetention) RunBeforeBackup(ctx context.Context, _ *xlog.StartupInfo) error {
	return ctx.Err()
}

func NewRetentionService(
	cfg *config.Config,
	opts *Opts, l *slog.Logger,
	basebackupStor st.Storage,
	walStor *st.VariadicStorage,
) RetentionService {
	if cfg == nil || !cfg.Retention.Enable {
		return NoopRetention{}
	}

	return &ConfiguredRetention{
		l:              l,
		cfg:            cfg,
		opts:           opts,
		basebackupStor: basebackupStor,
		walStor:        walStor,
	}
}

type ConfiguredRetention struct {
	l              *slog.Logger
	cfg            *config.Config
	opts           *Opts
	basebackupStor st.Storage
	walStor        *st.VariadicStorage
}

func (r *ConfiguredRetention) log() *slog.Logger {
	if r.l != nil {
		return r.l
	}

	return slog.With(slog.String("component", "basebackup-retention"))
}

func (r *ConfiguredRetention) RunBeforeBackup(
	ctx context.Context,
	startupInfo *xlog.StartupInfo,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if !r.cfg.Retention.Enable {
		return nil
	}

	switch r.cfg.Retention.Type {
	case config.RetentionTypeRecoveryWindow:
		retention := NewRecoveryWindowRetention(r.cfg, r.opts, r.log(), r.basebackupStor, r.walStor)
		return retention.RunBeforeBackup(ctx, startupInfo)

	default:
		return fmt.Errorf(
			"unsupported backup retention type %q: only %q is supported",
			r.cfg.Retention.Type,
			config.RetentionTypeRecoveryWindow,
		)
	}
}
