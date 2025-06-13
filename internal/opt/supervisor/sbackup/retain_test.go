package sbackup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFilterBackupsToDelete(t *testing.T) {
	now := time.Date(2025, 6, 13, 12, 0, 0, 0, time.UTC) // fixed 'now' for test stability
	keepPeriod := 48 * time.Hour                         // 2 days

	tests := []struct {
		name       string
		backupDirs []string
		wantDelete []string
	}{
		{
			name: "no backups to delete",
			backupDirs: []string{
				"20250613100000", // now
				"20250612120000", // inside 48h
				"20250611130000", // just inside 48h
			},
			wantDelete: []string{},
		},
		{
			name: "some backups to delete",
			backupDirs: []string{
				"20250613100000", // now
				"20250611100000", // inside 48h
				"20250610090000", // delete
				"20250609080000", // delete
			},
			wantDelete: []string{
				"20250609080000",
				"20250610090000",
				"20250611100000",
			},
		},
		{
			name: "all backups to delete",
			backupDirs: []string{
				"20250608080000",
				"20250607070000",
				"20250605050000",
			},
			wantDelete: []string{
				"20250605050000",
				"20250607070000",
				"20250608080000",
			},
		},
		{
			name: "invalid backup dir ignored",
			backupDirs: []string{
				"20250613100000", // valid, inside retention
				"invalid",        // invalid, should be skipped
				"20250609000000", // old → delete
			},
			wantDelete: []string{
				"20250609000000",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDelete := filterBackupsToDelete(tt.backupDirs, keepPeriod, now)
			assert.Equal(t, tt.wantDelete, gotDelete)
		})
	}
}
