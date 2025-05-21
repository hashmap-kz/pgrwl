package cmdutils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "host and port only",
			input:    "localhost:8080",
			expected: "http://localhost:8080",
		},
		{
			name:     "missing host",
			input:    ":9090",
			expected: "http://127.0.0.1:9090",
		},
		{
			name:     "http prefix",
			input:    "http://example.com:3000",
			expected: "http://example.com:3000",
		},
		{
			name:     "https prefix",
			input:    "https://example.com:443",
			expected: "https://example.com:443",
		},
		{
			name:    "invalid input",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := Addr(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, addr)
			}
		})
	}
}
