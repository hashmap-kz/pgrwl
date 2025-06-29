package receivemode

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
	//nolint:goconst
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

func TestFilterWalBefore_WithFullPaths(t *testing.T) {
	input := []string{
		"/mnt/wals/archive/00000001000000010000001A",
		"/mnt/wals/archive/00000001000000010000001B",
		"/mnt/wals/archive/00000001000000010000001C",
		"/mnt/wals/archive/00000001000000010000001D",
		"/mnt/wals/archive/00000001000000010000001E",
		"/mnt/wals/archive/00000001000000010000001F",
		"/mnt/wals/archive/000000010000000100000020",
	}
	cutoff := "00000001000000010000001F"
	expected := []string{
		"/mnt/wals/archive/00000001000000010000001A",
		"/mnt/wals/archive/00000001000000010000001B",
		"/mnt/wals/archive/00000001000000010000001C",
		"/mnt/wals/archive/00000001000000010000001D",
		"/mnt/wals/archive/00000001000000010000001E",
	}

	result := filterWalBefore(input, cutoff)
	assert.Equal(t, expected, result)
}

func TestFilterWalBefore_OnlyWalFiles(t *testing.T) {
	input := []string{
		"/mnt/wals/archive/00000001000000010000001A",        // WAL
		"/mnt/wals/archive/00000001000000010000001B.gz",     // WAL + .gz
		"/mnt/wals/archive/00000001000000010000001C.gz.aes", // WAL + .gz.aes
		"/mnt/wals/archive/00000001000000010000001D",        // WAL
		"/mnt/wals/archive/00000001000000010000001E",        // WAL
		"/mnt/wals/archive/00000001000000010000001F",        // WAL (cutoff)
		"/mnt/wals/archive/000000010000000100000020",        // WAL (after cutoff)
		"/mnt/wals/archive/00000001.history",                // history file (should be ignored)
		"/mnt/wals/archive/README.txt",                      // ignored
		"/mnt/wals/archive/foobar.gz",                       // ignored
		"/mnt/wals/archive/0000000100000001000000",          // too short
	}

	cutoff := "00000001000000010000001F"
	expected := []string{
		"/mnt/wals/archive/00000001000000010000001A",
		"/mnt/wals/archive/00000001000000010000001B.gz",
		"/mnt/wals/archive/00000001000000010000001C.gz.aes",
		"/mnt/wals/archive/00000001000000010000001D",
		"/mnt/wals/archive/00000001000000010000001E",
	}

	result := filterWalBefore(input, cutoff)
	assert.ElementsMatch(t, expected, result)
}
