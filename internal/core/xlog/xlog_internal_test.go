package xlog

import (
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
)

func TestIsPowerOf2(t *testing.T) {
	tests := []struct {
		input    uint64
		expected bool
	}{
		{0, false},
		{1, true},
		{2, true},
		{3, false},
		{4, true},
		{7, false},
		{8, true},
		{1024, true},
		{1023, false},
		{1 << 63, true},
		{(1 << 63) + 1, false},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, IsPowerOf2(tt.input), "input: %d", tt.input)
	}
}

func TestIsValidWalSegSize(t *testing.T) {
	valid := []uint64{1 << 20, 2 << 20, 4 << 20, 64 << 20, 1 << 30}
	invalid := []uint64{0, 3 << 20, 512 << 10, 2 << 30, 5 << 20}

	for _, size := range valid {
		assert.True(t, IsValidWalSegSize(size), "expected valid: %d", size)
	}
	for _, size := range invalid {
		assert.False(t, IsValidWalSegSize(size), "expected invalid: %d", size)
	}
}

func TestXLByteToSeg(t *testing.T) {
	assert.Equal(t, uint64(2), XLByteToSeg(32*1024*1024, 16*1024*1024))
	assert.Equal(t, uint64(0), XLByteToSeg(0, 16*1024*1024))
	assert.Equal(t, uint64(1), XLByteToSeg(17*1024*1024, 16*1024*1024))
}

func TestXLogSegmentOffset(t *testing.T) {
	lsn := pglogrepl.LSN((1 << 32) + 0x28) // timeline 1, offset 0x28
	offset := XLogSegmentOffset(lsn, 16*1024*1024)
	assert.Equal(t, uint64(0x28), offset)
}

func TestXLogSegmentsPerXLogId(t *testing.T) {
	assert.Equal(t, uint64(256), XLogSegmentsPerXLogId(16*1024*1024)) // 0x100000000 / 16MB
	assert.Equal(t, uint64(1024), XLogSegmentsPerXLogId(4*1024*1024)) // 1GB / 4MB
}

func TestXLogFileName(t *testing.T) {
	tli := uint32(1)
	seg := uint64(257)
	walSegSize := uint64(16 * 1024 * 1024)

	name := XLogFileName(tli, seg, walSegSize)

	// Expected hi = 1, lo = 1 (257 = 256 * 1 + 1)
	expected := "000000010000000100000001"
	assert.Equal(t, expected, name)
}

// wal file names

func TestIsXLogFileNameManual(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid", "0000000100000000000000A1", true},
		{"too short", "00000001", false},
		{"invalid hex", "0000000100000000000000ZZ", false},
		{"lowercase hex", "0000000100000000000000a1", false},
		{"too long", "0000000100000000000000A1X", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsXLogFileName(tt.input))
		})
	}
}

func TestIsPartialXLogFileName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid", "0000000100000000000000A1.partial", true},
		{"missing suffix", "0000000100000000000000A1", false},
		{"invalid hex", "0000000100000000000000ZZ.partial", false},
		{"lowercase hex", "0000000100000000000000a1.partial", false},
		{"wrong suffix", "0000000100000000000000A1.part", false},
		{"too long", "0000000100000000000000A1.partialx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsPartialXLogFileName(tt.input))
		})
	}
}

func TestXLogFromFileName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		walSegSize uint64
		wantTLI    uint32
		wantSegNo  uint64
		expectErr  bool
	}{
		{
			name:       "valid",
			input:      "0000000100000000000000A1",
			walSegSize: 16 * 1024 * 1024,
			wantTLI:    1,
			wantSegNo:  161,
			expectErr:  false,
		},
		{
			name:       "invalid hex",
			input:      "0000000100000000000000ZZ",
			walSegSize: 16 * 1024 * 1024,
			expectErr:  true,
		},
		{
			name:       "too short",
			input:      "00000001",
			walSegSize: 16 * 1024 * 1024,
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tli, segno, err := XLogFromFileName(tt.input, tt.walSegSize)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantTLI, tli)
				assert.Equal(t, tt.wantSegNo, segno)
			}
		})
	}
}
