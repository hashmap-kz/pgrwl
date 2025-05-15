package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandEnvsWithPrefix(t *testing.T) {
	// Set test environment variables
	t.Setenv("PGRWL_FOO", "foo-val")
	t.Setenv("PGRWL_BAR", "bar-val")
	t.Setenv("OTHER_BAZ", "should-not-expand")

	tests := []struct {
		name     string
		input    string
		prefix   string
		expected string
	}{
		{
			name:     "expand single matching var",
			input:    "value=${PGRWL_FOO}",
			prefix:   "PGRWL_",
			expected: "value=foo-val",
		},
		{
			name:     "expand multiple matching vars",
			input:    "one=${PGRWL_FOO}, two=${PGRWL_BAR}",
			prefix:   "PGRWL_",
			expected: "one=foo-val, two=bar-val",
		},
		{
			name:     "ignore unmatched var (wrong prefix)",
			input:    "value=${OTHER_BAZ}",
			prefix:   "PGRWL_",
			expected: "value=${OTHER_BAZ}",
		},
		{
			name:     "mixed matched and unmatched vars",
			input:    "a=${PGRWL_FOO}, b=${OTHER_BAZ}",
			prefix:   "PGRWL_",
			expected: "a=foo-val, b=${OTHER_BAZ}",
		},
		{
			name:     "undefined env var with correct prefix",
			input:    "value=${PGRWL_UNKNOWN}",
			prefix:   "PGRWL_",
			expected: "value=",
		},
		{
			name:     "empty input string",
			input:    "",
			prefix:   "PGRWL_",
			expected: "",
		},
		{
			name:     "no variable placeholders",
			input:    "static string",
			prefix:   "PGRWL_",
			expected: "static string",
		},
		{
			name:     "empty prefix allows all expansions",
			input:    "x=${PGRWL_FOO}, y=${OTHER_BAZ}",
			prefix:   "",
			expected: "x=foo-val, y=should-not-expand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := expandEnvsWithPrefix(tt.input, tt.prefix)
			assert.Equal(t, tt.expected, out)
		})
	}
}
