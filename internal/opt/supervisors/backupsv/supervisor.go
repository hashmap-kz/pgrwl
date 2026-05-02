package backupsv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/robfig/cron/v3"

	"github.com/pgrwl/pgrwl/config"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

var ErrBackupAlreadyRunning = errors.New("basebackup is already running")

type Opts struct {
	Directory string
	WalSegSz  uint64
}

type BaseBackupSupervisor interface {
	RunCron(ctx context.Context) error
	Trigger(ctx context.Context, source string) error
	TriggerAsync(ctx context.Context, source string) (*BackupRunState, error)
	BackupStatus() BackupRunState
}

type baseBackupSupervisor struct {
	l      *slog.Logger
	cfg    *config.Config
	opts   *Opts
	state  BackupState
	runner BackupRunner
	cron   *cron.Cron
}

var _ BaseBackupSupervisor = &baseBackupSupervisor{}

func NewBaseBackupSupervisor(
	cfg *config.Config,
	opts *Opts,
	basebackupStor st.Storage,
	walStor *st.VariadicStorage,
) BaseBackupSupervisor {
	if opts == nil {
		opts = &Opts{}
	}

	l := slog.With(slog.String("component", "basebackup-supervisor"))

	state := NewBackupState()

	retention := NewRetentionService(cfg, opts, l, basebackupStor, walStor)
	creator := &basebackupCreator{
		Directory: opts.Directory,
	}

	runner := NewBackupRunner(BackupRunnerOpts{
		Logger:     l,
		State:      state,
		Retention:  retention,
		Basebackup: creator,
	})

	return &baseBackupSupervisor{
		l:      l,
		cfg:    cfg,
		opts:   opts,
		state:  state,
		runner: runner,
		cron:   newBackupCron(),
	}
}

func (s *baseBackupSupervisor) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}

	return slog.With(slog.String("component", "basebackup-supervisor"))
}

// RunCron starts the basebackup scheduler and blocks until ctx is canceled.
//
// Fatal/setup errors are returned:
//   - cron expression is invalid
//
// Per-backup errors are logged and do not stop the scheduler:
//   - backup already running
//   - retention failed
//   - basebackup failed
//   - WAL cleanup failed
//   - panic inside a scheduled backup run
func (s *baseBackupSupervisor) RunCron(ctx context.Context) error {
	_, err := s.cron.AddFunc(s.cfg.Backup.Cron, func() {
		if err := s.runner.Run(ctx, "cron"); err != nil {
			s.handleRunError("scheduled", err)
		}
	})
	if err != nil {
		return fmt.Errorf("add basebackup cron job: %w", err)
	}

	s.cron.Start()

	s.log().Info("basebackup scheduler started",
		slog.String("cron", s.cfg.Backup.Cron),
	)

	<-ctx.Done()

	s.log().Info("stopping basebackup scheduler")

	stopCtx := s.cron.Stop()
	<-stopCtx.Done()

	s.log().Info("basebackup scheduler stopped")

	return nil
}

// Trigger starts a basebackup run synchronously.
func (s *baseBackupSupervisor) Trigger(ctx context.Context, source string) error {
	if source == "" {
		source = "manual"
	}

	return s.runner.Run(ctx, source)
}

// TriggerAsync starts a basebackup run in the background and returns the
// running state after the backup slot has been reserved.
//
// Pass the application context here, not the HTTP request context, otherwise
// the backup may be canceled as soon as the HTTP response is written.
func (s *baseBackupSupervisor) TriggerAsync(ctx context.Context, source string) (*BackupRunState, error) {
	if source == "" {
		source = "manual"
	}

	return s.runner.StartAsync(ctx, source)
}

func (s *baseBackupSupervisor) BackupStatus() BackupRunState {
	return s.state.Snapshot()
}

//nolint:unparam
func (s *baseBackupSupervisor) handleRunError(kind string, err error) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		s.log().Info(kind+" basebackup stopped", slog.Any("reason", err))

	case errors.Is(err, ErrBackupAlreadyRunning):
		s.log().Warn("previous basebackup still running, skipping this run")

	default:
		s.log().Error(kind+" basebackup run failed", slog.Any("err", err))
	}
}

func newBackupCron() *cron.Cron {
	// POSIX-compatible cron syntax: "* * * * *".
	// No seconds field.
	return cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
}
