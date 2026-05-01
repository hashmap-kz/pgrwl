package manualbackup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backup"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/backupsv"
)

type Gate interface {
	TryBeginBackup(source string) bool
	FinishBackup(status backupsv.BackupRunStatus, errMsg string)
	BackupStatus() backupsv.BackupRunState
}

type Service struct {
	l         *slog.Logger
	gate      Gate
	directory string
	appCtx    context.Context
}

type Options struct {
	Gate      Gate
	Directory string
	AppCtx    context.Context
}

func New(opts Options) *Service {
	return &Service{
		l:         slog.With("component", "manual-basebackup"),
		gate:      opts.Gate,
		directory: opts.Directory,
		appCtx:    opts.AppCtx,
	}
}

func (s *Service) Start(ctx context.Context) (*backupsv.BackupRunState, error) {
	if s.gate == nil {
		return nil, fmt.Errorf("backup gate is nil")
	}

	// Request already canceled before we accepted the trigger.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// App is shutting down.
	if err := s.appCtx.Err(); err != nil {
		return nil, err
	}

	if !s.gate.TryBeginBackup("manual") {
		return nil, backupsv.ErrBackupAlreadyRunning
	}

	state := s.gate.BackupStatus()

	go s.run(s.appCtx)

	return &state, nil
}

func (s *Service) Status() backupsv.BackupRunState {
	if s.gate == nil {
		return backupsv.BackupRunState{
			Running:   false,
			Status:    backupsv.BackupRunIdle,
			LastError: "backup gate is nil",
		}
	}

	return s.gate.BackupStatus()
}

// TODO: apply context
func (s *Service) run(_ context.Context) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic: %v", r)
			s.gate.FinishBackup(backupsv.BackupRunFailed, msg)
			s.l.Error("manual basebackup panicked", slog.Any("panic", r))
		}
	}()

	s.l.Info("starting manual basebackup")

	_, err := backup.CreateBaseBackup(&backup.CreateBaseBackupOpts{
		Directory: s.directory,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.gate.FinishBackup(backupsv.BackupRunFailed, "context canceled")
			s.l.Info("manual basebackup stopped", slog.Any("reason", err))
			return
		}

		s.gate.FinishBackup(backupsv.BackupRunFailed, err.Error())
		s.l.Error("manual basebackup failed", slog.Any("err", err))
		return
	}

	s.gate.FinishBackup(backupsv.BackupRunSucceeded, "")
	s.l.Info("manual basebackup completed")
}
