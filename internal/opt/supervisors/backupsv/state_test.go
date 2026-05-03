package backupsv

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupStateInitialSnapshot(t *testing.T) {
	state := NewBackupState()

	snap := state.Snapshot()

	assert.False(t, snap.Running)
	assert.Equal(t, BackupRunIdle, snap.Status)
	assert.Empty(t, snap.Source)
	assert.Nil(t, snap.StartedAt)
	assert.Nil(t, snap.FinishedAt)
	assert.Empty(t, snap.LastError)
}

func TestBackupStateBeginAndFinishSucceeded(t *testing.T) {
	state := NewBackupState()

	ok := state.Begin("manual")
	require.True(t, ok)

	running := state.Snapshot()
	assert.True(t, running.Running)
	assert.Equal(t, BackupRunRunning, running.Status)
	assert.Equal(t, "manual", running.Source)
	assert.NotNil(t, running.StartedAt)
	assert.Nil(t, running.FinishedAt)
	assert.Empty(t, running.LastError)

	state.Finish(BackupRunSucceeded, "")

	finished := state.Snapshot()
	assert.False(t, finished.Running)
	assert.Equal(t, BackupRunSucceeded, finished.Status)
	assert.Equal(t, "manual", finished.Source)
	assert.NotNil(t, finished.StartedAt)
	assert.NotNil(t, finished.FinishedAt)
	assert.Empty(t, finished.LastError)
	assert.False(t, finished.FinishedAt.Before(*finished.StartedAt))
}

func TestBackupStateBeginFailsWhileRunning(t *testing.T) {
	state := NewBackupState()

	require.True(t, state.Begin("cron"))
	assert.False(t, state.Begin("manual"))

	snap := state.Snapshot()
	assert.True(t, snap.Running)
	assert.Equal(t, "cron", snap.Source)
}

func TestBackupStateFinishFailedStoresErrorAndAllowsNextBegin(t *testing.T) {
	state := NewBackupState()

	require.True(t, state.Begin("cron"))
	state.Finish(BackupRunFailed, "boom")

	failed := state.Snapshot()
	assert.False(t, failed.Running)
	assert.Equal(t, BackupRunFailed, failed.Status)
	assert.Equal(t, "boom", failed.LastError)

	require.True(t, state.Begin("manual"))
	running := state.Snapshot()
	assert.True(t, running.Running)
	assert.Equal(t, BackupRunRunning, running.Status)
	assert.Equal(t, "manual", running.Source)
	assert.Empty(t, running.LastError)
}

func TestBackupStateSnapshotDeepCopiesTimePointers(t *testing.T) {
	state := NewBackupState()

	require.True(t, state.Begin("manual"))
	state.Finish(BackupRunSucceeded, "")

	snap1 := state.Snapshot()
	require.NotNil(t, snap1.StartedAt)
	require.NotNil(t, snap1.FinishedAt)

	started := *snap1.StartedAt
	finished := *snap1.FinishedAt

	// Mutate the returned snapshot. It must not mutate the state internals.
	*snap1.StartedAt = snap1.StartedAt.AddDate(100, 0, 0)
	*snap1.FinishedAt = snap1.FinishedAt.AddDate(100, 0, 0)

	snap2 := state.Snapshot()
	require.NotNil(t, snap2.StartedAt)
	require.NotNil(t, snap2.FinishedAt)

	assert.Equal(t, started, *snap2.StartedAt)
	assert.Equal(t, finished, *snap2.FinishedAt)
}

func TestBackupStateConcurrentBeginAllowsOnlyOneWinner(t *testing.T) {
	state := NewBackupState()

	const workers = 128

	var wg sync.WaitGroup
	var winners atomic.Int32

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if state.Begin("concurrent") {
				winners.Add(1)
			}
		}()
	}

	wg.Wait()

	assert.Equal(t, int32(1), winners.Load())

	snap := state.Snapshot()
	assert.True(t, snap.Running)
	assert.Equal(t, BackupRunRunning, snap.Status)
	assert.Equal(t, "concurrent", snap.Source)
}
