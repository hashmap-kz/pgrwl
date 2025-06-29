package backupsuperv

import (
	"slices"
	"sort"
	"time"
)

// filterBackupsToDeleteTimeBased returns a list of backup dir names to delete.
// backupDirs: list of dir names in "YYYYMMDDHHMMSS" format.
// keepPeriod: how long to retain backups.
// now: optional, if zero, uses time.Now().
func filterBackupsToDeleteTimeBased(backupDirs []string, keepPeriod time.Duration, now time.Time) []string {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-keepPeriod)

	toDelete := []string{}
	for _, dir := range backupDirs {
		t, err := time.Parse("20060102150405", dir)
		if err != nil {
			// Skip invalid dir names
			continue
		}
		if t.Before(cutoff) {
			toDelete = append(toDelete, dir)
		}
	}
	slices.Sort(toDelete)
	return toDelete
}

// filterBackupsToDeleteCountBased returns a list of backup dir names to delete.
// backupDirs: list of dir names in "YYYYMMDDHHMMSS" format.
// keepCnt: how many backups to keep
func filterBackupsToDeleteCountBased(backupDirs []string, keepLast int) []string {
	const layout = "20060102150405"

	type dirWithTime struct {
		name string
		t    time.Time
	}

	parsed := make([]dirWithTime, 0, len(backupDirs))
	for _, name := range backupDirs {
		t, err := time.Parse(layout, name)
		if err != nil {
			// Skip invalid format
			continue
		}
		parsed = append(parsed, dirWithTime{name, t})
	}

	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].t.After(parsed[j].t) // newest first
	})

	if keepLast >= len(parsed) {
		return nil
	}

	toDelete := make([]string, 0, len(parsed))
	for _, entry := range parsed[keepLast:] {
		toDelete = append(toDelete, entry.name)
	}

	return toDelete
}
