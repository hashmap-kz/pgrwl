package backupmetrics

import (
	"sync"
	"time"
)

var Tracker = &activeBackupTracker{
	backups: make(map[string]ActiveBackup),
}

const StuckThresholdDefault = 2 * time.Hour

type activeBackupTracker struct {
	mu        sync.RWMutex
	backups   map[string]ActiveBackup
	stuckFunc func(int)
}

func (t *activeBackupTracker) Start(id, cron string, startTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.backups[id] = ActiveBackup{
		ID:        id,
		Cron:      cron,
		StartTime: startTime,
	}
}

func (t *activeBackupTracker) Finish(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.backups, id)
}

func (t *activeBackupTracker) GetActive() []ActiveBackup {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]ActiveBackup, 0, len(t.backups))
	for _, b := range t.backups {
		result = append(result, b)
	}
	return result
}

func (t *activeBackupTracker) CountStuck(threshold time.Duration) int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, b := range t.backups {
		if now.Sub(b.StartTime) > threshold {
			count++
		}
	}
	return count
}
