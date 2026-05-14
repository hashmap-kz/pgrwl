package fakereaders

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetryableReader_RetryAfterFailure(t *testing.T) {
	failOnce := true

	r := NewRetryableReader(func() io.Reader {
		if failOnce {
			failOnce = false
			return NewFlakyReader(FlakyConfig{
				Size:   1024,
				FailAt: []int64{0},
			})
		}
		return NewFakeLargeReader(1024)
	})

	buf := make([]byte, 512)

	var total int64

	for {
		n, err := r.Read(buf)
		total += int64(n)

		if err == ErrFlaky {
			// simulate retry
			continue
		}

		if err == io.EOF {
			break
		}

		require.NoError(t, err)
	}

	require.Equal(t, int64(1024), total)
}

func TestRetryableReader_NoResetAfterPartialRead(t *testing.T) {
	r := NewRetryableReader(func() io.Reader {
		return NewFlakyReader(FlakyConfig{
			Size:   1024,
			FailAt: []int64{512},
		})
	})

	buf := make([]byte, 256)

	var total int64
	failSeen := false

	for {
		n, err := r.Read(buf)
		total += int64(n)

		if err == ErrFlaky {
			failSeen = true
			continue
		}

		if err == io.EOF {
			break
		}
	}

	require.True(t, failSeen)

	// IMPORTANT: should NOT duplicate data
	require.Equal(t, int64(1024), total)
}

func TestRetryableReader_InfiniteFailure(t *testing.T) {
	r := NewRetryableReader(func() io.Reader {
		return NewFlakyReader(FlakyConfig{
			Size:   1024,
			FailAt: []int64{0},
		})
	})

	buf := make([]byte, 128)

	_, err := r.Read(buf)

	require.Error(t, err)
}

func TestRetryableReader_MultipleRetries(t *testing.T) {
	attempt := 0

	r := NewRetryableReader(func() io.Reader {
		attempt++

		if attempt < 3 {
			return NewFlakyReader(FlakyConfig{
				Size:   1024,
				FailAt: []int64{0},
			})
		}

		return NewFakeLargeReader(1024)
	})

	n, err := io.Copy(io.Discard, r)

	require.NoError(t, err)
	require.Equal(t, int64(1024), n)
	require.Equal(t, 3, attempt)
}
