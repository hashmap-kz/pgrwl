package backup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadCString(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		wantStr     string
		wantRemain  []byte
		expectError bool
	}{
		{
			name:        "valid CString with remaining bytes",
			input:       []byte("hello\x00world"),
			wantStr:     "hello",
			wantRemain:  []byte("world"),
			expectError: false,
		},
		{
			name:        "valid CString with empty remainder",
			input:       []byte("test\x00"),
			wantStr:     "test",
			wantRemain:  []byte{},
			expectError: false,
		},
		{
			name:        "CString at beginning of buffer",
			input:       []byte("\x00rest"),
			wantStr:     "",
			wantRemain:  []byte("rest"),
			expectError: false,
		},
		{
			name:        "no null terminator",
			input:       []byte("nonull"),
			wantStr:     "",
			wantRemain:  nil,
			expectError: true,
		},
		{
			name:        "only null byte",
			input:       []byte("\x00"),
			wantStr:     "",
			wantRemain:  []byte{},
			expectError: false,
		},
		{
			name:        "null at end of buffer",
			input:       []byte("abc\x00"),
			wantStr:     "abc",
			wantRemain:  []byte{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStr, gotRemain, err := readCString(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantStr, gotStr)
				assert.Equal(t, tt.wantRemain, gotRemain)
			}
		})
	}
}
