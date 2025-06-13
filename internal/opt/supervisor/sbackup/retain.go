package sbackup

import (
	"slices"
	"time"
)

// filterBackupsToDelete returns a list of backup dir names to delete.
// backupDirs: list of dir names in "YYYYMMDDHHMMSS" format.
// keepPeriod: how long to retain backups.
// now: optional, if zero, uses time.Now().
func filterBackupsToDelete(backupDirs []string, keepPeriod time.Duration, now time.Time) []string {
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
