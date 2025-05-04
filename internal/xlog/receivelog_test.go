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
			stream:                &StreamCtl{stillSending: true},
			standbyMessageTimeout: 0,
			lastStatus:            now,
			expected:              -1,
		},
		{
			name:                  "Not still sending",
			stream:                &StreamCtl{stillSending: false},
			standbyMessageTimeout: 10 * time.Second,
			lastStatus:            now,
			expected:              -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stream.calculateCopyStreamSleepTime(now)
			assert.Equal(t, tt.expected, got)
		})
	}
}
