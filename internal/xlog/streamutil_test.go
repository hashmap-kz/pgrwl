package xlog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanWalSegSize_ValidInputs(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"16MB", 16 * 1024 * 1024},
		{"1GB", 1 * 1024 * 1024 * 1024},
		{"32MB", 32 * 1024 * 1024},
		{"2GB", 2 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		got, err := ScanWalSegSize(tt.input)
		assert.NoError(t, err, "input: %s", tt.input)
		assert.Equal(t, tt.expected, got, "input: %s", tt.input)
	}
}

func TestScanWalSegSize_OutOfRange(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"2GB", 2 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		_, err := ScanWalSegSize(tt.input)
		assert.Error(t, err, "input: %s", tt.input)
	}
}

func TestScanWalSegSize_InvalidInputs(t *testing.T) {
	invalidInputs := []string{
		"",         // empty
		"MB",       // missing number
		"16",       // missing unit
		"abcMB",    // invalid number
		"16mB",     // wrong case
		"16m",      // invalid unit
		"16foobar", // unknown unit
	}

	for _, input := range invalidInputs {
		got, err := ScanWalSegSize(input)
		assert.Error(t, err, "expected error for input: %s", input)
		assert.Equal(t, uint64(0), got, "expected 0 for input: %s", input)
	}
}
