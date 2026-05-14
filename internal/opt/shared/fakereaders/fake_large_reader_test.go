package fakereaders

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFakeLargeReader_ReadAll(t *testing.T) {
	r := NewFakeLargeReader(10 << 20)

	n, err := io.Copy(io.Discard, r)
	require.NoError(t, err)
	require.Equal(t, int64(10<<20), n)
}

func TestFakeLargeReader_ExactEOF(t *testing.T) {
	r := NewFakeLargeReader(1)

	buf := make([]byte, 10)

	n, err := r.Read(buf)
	require.Equal(t, 1, n)
	require.NoError(t, err)

	n, err = r.Read(buf)
	require.Equal(t, 0, n)
	require.Equal(t, io.EOF, err)
}
