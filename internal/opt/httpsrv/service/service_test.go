package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterWalBefore(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		cutoff   string
		expected []string
	}{
		{
			name:     "basic case with some before and some after",
			input:    []string{"00000001000000010000001A", "00000001000000010000001F", "000000010000000100000020"},
			cutoff:   "00000001000000010000001F",
			expected: []string{"00000001000000010000001A"},
		},
		{
			name:     "all before cutoff",
			input:    []string{"00000001000000010000001A", "00000001000000010000001B"},
			cutoff:   "00000001000000010000001C",
			expected: []string{"00000001000000010000001A", "00000001000000010000001B"},
		},
		{
			name:     "all after cutoff",
			input:    []string{"00000001000000010000001F", "000000010000000100000020"},
			cutoff:   "00000001000000010000001E",
			expected: []string{},
		},
		{
			name:     "cutoff is lowest possible WAL name",
			input:    []string{"00000001000000010000001A", "00000001000000010000001B"},
			cutoff:   "000000000000000000000000",
			expected: []string{},
		},
		{
			name:     "cutoff is highest possible WAL name",
			input:    []string{"00000001000000010000001A", "FFFFFFFFFFFFFFFFFFFFFFFF"},
			cutoff:   "FFFFFFFFFFFFFFFFFFFFFFFF",
			expected: []string{"00000001000000010000001A"},
		},
		{
			name:     "input contains duplicate values",
			input:    []string{"00000001000000010000001A", "00000001000000010000001A", "00000001000000010000001B"},
			cutoff:   "00000001000000010000001B",
			expected: []string{"00000001000000010000001A", "00000001000000010000001A"},
		},
		{
			name:     "empty input",
			input:    []string{},
			cutoff:   "00000001000000010000001F",
			expected: []string{},
		},
		{
			name:     "cutoff is included in input",
			input:    []string{"00000001000000010000001E", "00000001000000010000001F", "000000010000000100000020"},
			cutoff:   "00000001000000010000001F",
			expected: []string{"00000001000000010000001E"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := filterWalBefore(tt.input, tt.cutoff)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestFilterWalBefore_StrictCutoff(t *testing.T) {
	input := []string{
		"00000001000000010000001A",
		"00000001000000010000001B",
		"00000001000000010000001C",
		"00000001000000010000001D",
		"00000001000000010000001E",
		"00000001000000010000001F",
		"000000010000000100000020",
		"000000010000000100000021",
		"000000010000000100000022",
		"000000010000000100000023",
	}
	cutoff := "00000001000000010000001F"
	expected := []string{
		"00000001000000010000001A",
		"00000001000000010000001B",
		"00000001000000010000001C",
		"00000001000000010000001D",
		"00000001000000010000001E",
	}

	result := filterWalBefore(input, cutoff)
	assert.Equal(t, expected, result)
}
