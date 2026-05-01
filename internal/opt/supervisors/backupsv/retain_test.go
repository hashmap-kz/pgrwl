package backupsv

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	assert.NoError(t, err)

	return parsed
}

func makeBackup(name string, startedAt time.Time, beginWAL string) recoveryWindowBackup {
	return recoveryWindowBackup{
		name:      name,
		path:      name,
		startedAt: startedAt,
		beginWAL:  beginWAL,
	}
}

func TestChooseRecoveryWindowAnchor(t *testing.T) {
	now := mustTime(t, "2026-04-30T18:00:00Z")

	tests := []struct {
		name           string
		backups        []recoveryWindowBackup
		recoveryWindow time.Duration
		minimumBackups int
		wantAnchor     string
	}{
		{
			name:           "no backups",
			backups:        nil,
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "",
		},
		{
			name: "barman-like window chooses newest backup older than window start",
			backups: []recoveryWindowBackup{
				makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
				makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
				makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 72 * time.Hour, // window start: 2026-04-27T18:00:00Z
			minimumBackups: 1,
			wantAnchor:     "20260425000000",
		},
		{
			name: "backup exactly at window start can be anchor",
			backups: []recoveryWindowBackup{
				makeBackup("20260427180000", mustTime(t, "2026-04-27T18:00:00Z"), "000000010000001100000001"),
				makeBackup("20260429000000", mustTime(t, "2026-04-29T00:00:00Z"), "000000010000001200000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "20260427180000",
		},
		{
			name: "if all backups are newer than window start, choose oldest backup",
			backups: []recoveryWindowBackup{
				makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
				makeBackup("20260429000000", mustTime(t, "2026-04-29T00:00:00Z"), "000000010000001300000001"),
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001400000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "20260428000000",
		},
		{
			name: "minimumBackups moves anchor backwards",
			backups: []recoveryWindowBackup{
				makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
				makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
				makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 24 * time.Hour, // window start: 2026-04-29T18:00:00Z, anchor would be 20260428000000
			minimumBackups: 3,              // must keep Apr25, Apr28, Apr30
			wantAnchor:     "20260425000000",
		},
		{
			name: "minimumBackups greater than available backups chooses oldest",
			backups: []recoveryWindowBackup{
				makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 24 * time.Hour,
			minimumBackups: 10,
			wantAnchor:     "20260428000000",
		},
		{
			name: "minimumBackups zero defaults to one",
			backups: []recoveryWindowBackup{
				makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
				makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 0,
			wantAnchor:     "20260425000000",
		},
		{
			name: "input order does not matter",
			backups: []recoveryWindowBackup{
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
				makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
				makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
				makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "20260425000000",
		},
		{
			name: "single backup is always anchor",
			backups: []recoveryWindowBackup{
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "20260430000000",
		},
		{
			name: "zero now uses time.Now and still returns some anchor",
			backups: []recoveryWindowBackup{
				makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
			},
			recoveryWindow: 72 * time.Hour,
			minimumBackups: 1,
			wantAnchor:     "20260430000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testNow := now
			if tt.name == "zero now uses time.Now and still returns some anchor" {
				testNow = time.Time{}
			}

			got := chooseRecoveryWindowAnchor(
				tt.backups,
				tt.recoveryWindow,
				tt.minimumBackups,
				testNow,
			)

			if tt.wantAnchor == "" {
				assert.Nil(t, got)
				return
			}

			if assert.NotNil(t, got) {
				assert.Equal(t, tt.wantAnchor, got.name)
			}
		})
	}
}

func TestBackupsOlderThanAnchor(t *testing.T) {
	backups := []recoveryWindowBackup{
		makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
		makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
		makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
		makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
	}

	t.Run("nil anchor returns nil", func(t *testing.T) {
		got := backupsOlderThanAnchor(backups, nil)
		assert.Nil(t, got)
	})

	t.Run("returns only backups older than anchor", func(t *testing.T) {
		anchor := &recoveryWindowBackup{
			name:      "20260428000000",
			startedAt: mustTime(t, "2026-04-28T00:00:00Z"),
		}

		got := backupsOlderThanAnchor(backups, anchor)

		assert.Equal(t, []string{
			"20260420000000",
			"20260425000000",
		}, got)
	})

	t.Run("oldest anchor deletes nothing", func(t *testing.T) {
		anchor := &recoveryWindowBackup{
			name:      "20260420000000",
			startedAt: mustTime(t, "2026-04-20T00:00:00Z"),
		}

		got := backupsOlderThanAnchor(backups, anchor)

		assert.Empty(t, got)
	})

	t.Run("newest anchor deletes all older backups", func(t *testing.T) {
		anchor := &recoveryWindowBackup{
			name:      "20260430000000",
			startedAt: mustTime(t, "2026-04-30T00:00:00Z"),
		}

		got := backupsOlderThanAnchor(backups, anchor)

		assert.Equal(t, []string{
			"20260420000000",
			"20260425000000",
			"20260428000000",
		}, got)
	})

	t.Run("result is sorted even when input is unordered", func(t *testing.T) {
		unordered := []recoveryWindowBackup{
			backups[3],
			backups[1],
			backups[0],
			backups[2],
		}

		anchor := &recoveryWindowBackup{
			name:      "20260430000000",
			startedAt: mustTime(t, "2026-04-30T00:00:00Z"),
		}

		got := backupsOlderThanAnchor(unordered, anchor)

		assert.Equal(t, []string{
			"20260420000000",
			"20260425000000",
			"20260428000000",
		}, got)
	})
}

func TestNormalizeWALFilename(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantName    string
		wantHistory bool
		wantOK      bool
	}{
		{
			name:        "plain WAL",
			path:        "00000001000000140000003B",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "lowercase WAL is normalized to uppercase",
			path:        "00000001000000140000003b",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with gzip extension",
			path:        "00000001000000140000003B.gz",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with zstd extension",
			path:        "00000001000000140000003B.zst",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with lz4 extension",
			path:        "00000001000000140000003B.lz4",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with aes extension",
			path:        "00000001000000140000003B.aes",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with chained gzip and aes extensions",
			path:        "00000001000000140000003B.gz.aes",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "WAL with chained zstd and aes extensions",
			path:        "00000001000000140000003B.zst.aes",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "path uses base filename only",
			path:        "archive/wals/00000001000000140000003B.gz.aes",
			wantName:    "00000001000000140000003B",
			wantHistory: false,
			wantOK:      true,
		},
		{
			name:        "timeline history file",
			path:        "00000002.history",
			wantName:    "00000002.history",
			wantHistory: true,
			wantOK:      true,
		},
		{
			name:        "timeline history file with path",
			path:        "archive/00000002.history",
			wantName:    "00000002.history",
			wantHistory: true,
			wantOK:      true,
		},
		{
			name:        "invalid too short",
			path:        "00000001000000140000003",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "invalid too long",
			path:        "00000001000000140000003B0",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "invalid hex",
			path:        "00000001000000140000003X",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "random file",
			path:        "README.md",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "manifest file should not be treated as WAL",
			path:        "manifest.json",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "partial file is not handled by this normalizer",
			path:        "00000001000000140000003B.partial",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
		{
			name:        "partial encrypted file is not handled by this normalizer",
			path:        "00000001000000140000003B.partial.gz.aes",
			wantName:    "",
			wantHistory: false,
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotHistory, gotOK := normalizeWALFilename(tt.path)

			assert.Equal(t, tt.wantName, gotName)
			assert.Equal(t, tt.wantHistory, gotHistory)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestWalBefore(t *testing.T) {
	tests := []struct {
		name     string
		wal      string
		boundary string
		want     bool
	}{
		{
			name:     "wal before boundary",
			wal:      "00000001000000140000003A",
			boundary: "00000001000000140000003B",
			want:     true,
		},
		{
			name:     "wal equal boundary is not before",
			wal:      "00000001000000140000003B",
			boundary: "00000001000000140000003B",
			want:     false,
		},
		{
			name:     "wal after boundary",
			wal:      "00000001000000140000003C",
			boundary: "00000001000000140000003B",
			want:     false,
		},
		{
			name:     "case-insensitive compare",
			wal:      "00000001000000140000003a",
			boundary: "00000001000000140000003B",
			want:     true,
		},
		{
			name:     "empty wal",
			wal:      "",
			boundary: "00000001000000140000003B",
			want:     false,
		},
		{
			name:     "empty boundary",
			wal:      "00000001000000140000003A",
			boundary: "",
			want:     false,
		},
		{
			name:     "trim spaces",
			wal:      " 00000001000000140000003A ",
			boundary: " 00000001000000140000003B ",
			want:     true,
		},
		{
			name:     "timeline lower can be before",
			wal:      "00000001000000140000003B",
			boundary: "00000002000000140000003B",
			want:     true,
		},
		{
			name:     "timeline higher is after",
			wal:      "00000002000000140000003B",
			boundary: "00000001000000140000003B",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := walBefore(tt.wal, tt.boundary)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRecoveryWindowScenario(t *testing.T) {
	now := mustTime(t, "2026-04-30T18:00:00Z")

	backups := []recoveryWindowBackup{
		makeBackup("20260420000000", mustTime(t, "2026-04-20T00:00:00Z"), "000000010000001000000001"),
		makeBackup("20260425000000", mustTime(t, "2026-04-25T00:00:00Z"), "000000010000001100000001"),
		makeBackup("20260428000000", mustTime(t, "2026-04-28T00:00:00Z"), "000000010000001200000001"),
		makeBackup("20260430000000", mustTime(t, "2026-04-30T00:00:00Z"), "000000010000001300000001"),
	}

	anchor := chooseRecoveryWindowAnchor(backups, 72*time.Hour, 1, now)
	if !assert.NotNil(t, anchor) {
		return
	}

	assert.Equal(t, "20260425000000", anchor.name)
	assert.Equal(t, "000000010000001100000001", anchor.beginWAL)

	toDelete := backupsOlderThanAnchor(backups, anchor)

	assert.Equal(t, []string{
		"20260420000000",
	}, toDelete)

	rawWALs := []string{
		"000000010000001000000001",
		"000000010000001000000002.gz",
		"000000010000001100000000",
		"000000010000001100000001",
		"000000010000001100000002",
		"00000002.history",
	}

	deleteWALs := make([]string, 0)
	keepWALs := make([]string, 0)

	for _, raw := range rawWALs {
		name, history, ok := normalizeWALFilename(raw)
		if !ok || history || !walBefore(name, anchor.beginWAL) {
			keepWALs = append(keepWALs, raw)
			continue
		}

		deleteWALs = append(deleteWALs, raw)
	}

	assert.Equal(t, []string{
		"000000010000001000000001",
		"000000010000001000000002.gz",
		"000000010000001100000000",
	}, deleteWALs)

	assert.Equal(t, []string{
		"000000010000001100000001",
		"000000010000001100000002",
		"00000002.history",
	}, keepWALs)
}
