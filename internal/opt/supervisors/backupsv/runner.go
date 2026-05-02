package backupsv

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/pgrwl/pgrwl/internal/core/xlog"
)

type BackupRunnerOpts struct {
	Logger      *slog.Logger
	State       BackupState
	Retention   RetentionService
	Basebackup  BaseBackupCreator
	StartupInfo *xlog.StartupInfo
}

type BackupRunner interface {
	Run(ctx context.Context, source string) error
	StartAsync(ctx context.Context, source string) (*BackupRunState, error)
	SetStartupInfo(info *xlog.StartupInfo)
	StartupInfo() *xlog.StartupInfo
}

type backupRunner struct {
	l *slog.Logger

	state      BackupState
	retention  RetentionService
	basebackup BaseBackupCreator

	startupMu   sync.RWMutex
	startupInfo *xlog.StartupInfo
}

var _ BackupRunner = &backupRunner{}

func NewBackupRunner(opts BackupRunnerOpts) BackupRunner {
	l := opts.Logger
	if l == nil {
		l = slog.With(slog.String("component", "basebackup-runner"))
	}

	return &backupRunner{
		l:           l,
		state:       opts.State,
		retention:   opts.Retention,
		basebackup:  opts.Basebackup,
		startupInfo: opts.StartupInfo,
	}
}

func (r *backupRunner) SetStartupInfo(info *xlog.StartupInfo) {
	r.startupMu.Lock()
	r.startupInfo = info
	r.startupMu.Unlock()
}

func (r *backupRunner) StartupInfo() *xlog.StartupInfo {
	r.startupMu.RLock()
	defer r.startupMu.RUnlock()

	return r.startupInfo
}

func (r *backupRunner) Run(ctx context.Context, source string) error {
	if _, err := r.reserve(ctx, source); err != nil {
		return err
	}

	return r.runReserved(ctx, source)
}

func (r *backupRunner) StartAsync(ctx context.Context, source string) (*BackupRunState, error) {
	state, err := r.reserve(ctx, source)
	if err != nil {
		return nil, err
	}

	go func() {
		defer func() {
			if v := recover(); v != nil {
				errMsg := fmt.Sprintf("panic: %v", v)

				r.l.Error("async basebackup run panicked",
					slog.String("source", source),
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)

				r.state.Finish(BackupRunFailed, errMsg)
			}
		}()

		if err := r.runReserved(ctx, source); err != nil {
			r.l.Error("async basebackup run failed",
				slog.String("source", source),
				slog.Any("err", err),
			)
		}
	}()

	return state, nil
}

func (r *backupRunner) reserve(ctx context.Context, source string) (*BackupRunState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if r.StartupInfo() == nil {
		return nil, fmt.Errorf("startup info is not loaded")
	}

	if !r.state.Begin(source) {
		return nil, ErrBackupAlreadyRunning
	}

	state := r.state.Snapshot()
	return &state, nil
}

func (r *backupRunner) runReserved(ctx context.Context, source string) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("basebackup panicked: %v", rec)
		}

		if err != nil {
			r.state.Finish(BackupRunFailed, err.Error())
			return
		}

		r.state.Finish(BackupRunSucceeded, "")
	}()

	r.l.Info("starting basebackup",
		slog.String("source", source),
	)

	startupInfo := r.StartupInfo()
	if startupInfo == nil {
		return fmt.Errorf("startup info is not loaded")
	}

	if err := r.retention.RunBeforeBackup(ctx, startupInfo); err != nil {
		return fmt.Errorf("retention before basebackup: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := r.basebackup.Create(ctx); err != nil {
		return fmt.Errorf("create basebackup: %w", err)
	}

	r.l.Info("basebackup completed",
		slog.String("source", source),
	)

	return nil
}
