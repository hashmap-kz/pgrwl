package xlog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateCopyStreamSleepTime(t *testing.T) {
	now := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name                  string
		stream                *StreamCtl
		standbyMessageTimeout time.Duration
		lastStatus            time.Time
		expected              time.Duration
	}{
		{
			name:                  "No timeout configured",
			stream:                &StreamCtl{StillSending: true},
			standbyMessageTimeout: 0,
			lastStatus:            now,
			expected:              -1,
		},
		{
			name:                  "Not still sending",
			stream:                &StreamCtl{StillSending: false},
			standbyMessageTimeout: 10 * time.Second,
			lastStatus:            now,
			expected:              -1,
		},
		{
			name:                  "Timeout exactly now",
			stream:                &StreamCtl{StillSending: true},
			standbyMessageTimeout: 10 * time.Second,
			lastStatus:            now.Add(-10 * time.Second),
			expected:              1 * time.Second,
		},
		{
			name:                  "Timeout in the past",
			stream:                &StreamCtl{StillSending: true},
			standbyMessageTimeout: 10 * time.Second,
			lastStatus:            now.Add(-20 * time.Second),
			expected:              1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCopyStreamSleepTime(tt.stream, now, tt.standbyMessageTimeout)
			assert.Equal(t, tt.expected, got)
		})
	}
}
