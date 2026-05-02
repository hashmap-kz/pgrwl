package backupsv

import (
	"sync"
	"sync/atomic"
	"time"
)

type BackupRunStatus string

const (
	BackupRunIdle      BackupRunStatus = "idle"
	BackupRunRunning   BackupRunStatus = "running"
	BackupRunSucceeded BackupRunStatus = "succeeded"
	BackupRunFailed    BackupRunStatus = "failed"
)

type BackupRunState struct {
	Running    bool            `json:"running"`
	Status     BackupRunStatus `json:"status"`
	Source     string          `json:"source,omitempty"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
}

type BackupState interface {
	Begin(source string) bool
	Finish(status BackupRunStatus, errMsg string)
	Snapshot() BackupRunState
}

type backupState struct {
	mu      sync.RWMutex
	running int32
	state   BackupRunState
}

var _ BackupState = &backupState{}

func NewBackupState() BackupState {
	return &backupState{
		state: BackupRunState{
			Running: false,
			Status:  BackupRunIdle,
		},
	}
}

// Begin atomically reserves the single backup slot.
//
// Both cron and manual REST-triggered backups must call this before doing
// any backup work. This avoids a check-then-start race.
func (s *backupState) Begin(source string) bool {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return false
	}

	now := time.Now().UTC()

	s.mu.Lock()
	s.state = BackupRunState{
		Running:    true,
		Status:     BackupRunRunning,
		Source:     source,
		StartedAt:  &now,
		FinishedAt: nil,
		LastError:  "",
	}
	s.mu.Unlock()

	return true
}

// Finish releases the single backup slot and stores the final state.
// It must be called exactly once after successful begin().
func (s *backupState) Finish(status BackupRunStatus, errMsg string) {
	now := time.Now().UTC()

	s.mu.Lock()
	s.state.Running = false
	s.state.Status = status
	s.state.FinishedAt = &now
	s.state.LastError = errMsg
	s.mu.Unlock()

	atomic.StoreInt32(&s.running, 0)
}

func (s *backupState) Snapshot() BackupRunState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneBackupRunState(s.state)
}

func cloneBackupRunState(in BackupRunState) BackupRunState {
	out := in

	if in.StartedAt != nil {
		t := *in.StartedAt
		out.StartedAt = &t
	}

	if in.FinishedAt != nil {
		t := *in.FinishedAt
		out.FinishedAt = &t
	}

	return out
}
