package sbackup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFindOldBackupsToDeleteTimeBased(t *testing.T) {
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
			gotDelete := filterBackupsToDeleteTimeBased(tt.backupDirs, keepPeriod, now)
			assert.Equal(t, tt.wantDelete, gotDelete)
		})
	}
}

func TestFindOldBackupsToDeleteCountBased(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		keepLast int
		expected []string
	}{
		{
			name: "keep 2 newest backups",
			input: []string{
				"20240610010101",
				"20240611010101",
				"20240612010101",
			},
			keepLast: 2,
			expected: []string{"20240610010101"},
		},
		{
			name: "keep all backups",
			input: []string{
				"20240610010101",
				"20240611010101",
			},
			keepLast: 2,
			expected: nil,
		},
		{
			name: "keep more than available",
			input: []string{
				"20240610010101",
			},
			keepLast: 5,
			expected: nil,
		},
		{
			name: "input with invalid formats",
			input: []string{
				"20240610010101",
				"invalid-dirname",
				"20240609010101",
			},
			keepLast: 1,
			expected: []string{"20240609010101"},
		},
		{
			name: "keep 0",
			input: []string{
				"20240610010101",
				"20240611010101",
				"20240612010101",
			},
			keepLast: 0,
			expected: []string{
				"20240612010101",
				"20240611010101",
				"20240610010101",
			},
		},
		{
			name:     "empty input",
			input:    []string{},
			keepLast: 2,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterBackupsToDeleteCountBased(tt.input, tt.keepLast)
			assert.ElementsMatch(t, tt.expected, result)
		})
	}
}
