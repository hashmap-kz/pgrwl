package backupsv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackupRunner struct {
	runCalls        int
	startAsyncCalls int
	lastSource      string
	runErr          error
	startAsyncErr   error
	state           BackupRunState
	startupInfo     *xlog.StartupInfo
}

var _ BackupRunner = (*fakeBackupRunner)(nil)

func (r *fakeBackupRunner) Run(_ context.Context, source string) error {
	r.runCalls++
	r.lastSource = source
	return r.runErr
}

func (r *fakeBackupRunner) StartAsync(_ context.Context, source string) (*BackupRunState, error) {
	r.startAsyncCalls++
	r.lastSource = source
	if r.startAsyncErr != nil {
		return nil, r.startAsyncErr
	}
	state := r.state
	if state.Status == "" {
		state = BackupRunState{Running: true, Status: BackupRunRunning, Source: source}
	}
	return &state, nil
}

func (r *fakeBackupRunner) SetStartupInfo(info *xlog.StartupInfo) { r.startupInfo = info }
func (r *fakeBackupRunner) StartupInfo() *xlog.StartupInfo        { return r.startupInfo }

func newSupervisorForTest(state BackupState, runner BackupRunner) *baseBackupSupervisor {
	return &baseBackupSupervisor{
		l:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:    &config.Config{Backup: config.BackupConfig{Cron: "* * * * *"}},
		opts:   &Opts{},
		state:  state,
		runner: runner,
		cron:   cron.New(),
	}
}

func TestBaseBackupSupervisorTriggerDefaultsSourceToManual(t *testing.T) {
	state := NewBackupState()
	runner := &fakeBackupRunner{}
	s := newSupervisorForTest(state, runner)

	err := s.Trigger(context.Background(), "")

	require.NoError(t, err)
	assert.Equal(t, 1, runner.runCalls)
	assert.Equal(t, "manual", runner.lastSource)
}

func TestBaseBackupSupervisorTriggerPassesExplicitSource(t *testing.T) {
	runner := &fakeBackupRunner{}
	s := newSupervisorForTest(NewBackupState(), runner)

	err := s.Trigger(context.Background(), "cron")

	require.NoError(t, err)
	assert.Equal(t, "cron", runner.lastSource)
}

func TestBaseBackupSupervisorTriggerPropagatesRunnerError(t *testing.T) {
	runner := &fakeBackupRunner{runErr: errors.New("run failed")}
	s := newSupervisorForTest(NewBackupState(), runner)

	err := s.Trigger(context.Background(), "manual")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run failed")
}

func TestBaseBackupSupervisorTriggerAsyncDefaultsSourceToManual(t *testing.T) {
	runner := &fakeBackupRunner{}
	s := newSupervisorForTest(NewBackupState(), runner)

	state, err := s.TriggerAsync(context.Background(), "")

	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 1, runner.startAsyncCalls)
	assert.Equal(t, "manual", runner.lastSource)
	assert.True(t, state.Running)
}

func TestBaseBackupSupervisorTriggerAsyncPropagatesRunnerError(t *testing.T) {
	runner := &fakeBackupRunner{startAsyncErr: ErrBackupAlreadyRunning}
	s := newSupervisorForTest(NewBackupState(), runner)

	state, err := s.TriggerAsync(context.Background(), "manual")

	assert.Nil(t, state)
	assert.ErrorIs(t, err, ErrBackupAlreadyRunning)
}

func TestBaseBackupSupervisorBackupStatusReturnsStateSnapshot(t *testing.T) {
	state := NewBackupState()
	require.True(t, state.Begin("manual"))
	s := newSupervisorForTest(state, &fakeBackupRunner{})

	snap := s.BackupStatus()

	assert.True(t, snap.Running)
	assert.Equal(t, BackupRunRunning, snap.Status)
	assert.Equal(t, "manual", snap.Source)
}

func TestBaseBackupSupervisorHandleRunErrorDoesNotPanic(t *testing.T) {
	s := newSupervisorForTest(NewBackupState(), &fakeBackupRunner{})

	assert.NotPanics(t, func() {
		s.handleRunError("scheduled", context.Canceled)
		s.handleRunError("scheduled", context.DeadlineExceeded)
		s.handleRunError("scheduled", ErrBackupAlreadyRunning)
		s.handleRunError("scheduled", errors.New("boom"))
	})
}
