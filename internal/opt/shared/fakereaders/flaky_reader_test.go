package fakereaders

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlakyReader_FailsAtExactOffset(t *testing.T) {
	cfg := FlakyConfig{
		Size:   1024,
		FailAt: []int64{512},
	}

	r := NewFlakyReader(cfg)

	buf := make([]byte, 256)

	var total int64

	for {
		n, err := r.Read(buf)
		total += int64(n)

		if err != nil {
			require.ErrorIs(t, err, ErrFlaky)
			break
		}
	}

	require.Equal(t, int64(512), total)
}

func TestFlakyReader_MultipleFailures(t *testing.T) {
	cfg := FlakyConfig{
		Size:           1024,
		FailAt:         []int64{256, 512, 768},
		RepeatFailures: true,
	}

	r := NewFlakyReader(cfg)

	buf := make([]byte, 128)

	failCount := 0

	for {
		_, err := r.Read(buf)

		if err == ErrFlaky {
			failCount++
			continue
		}

		if err == io.EOF {
			break
		}
	}

	require.Equal(t, 3, failCount)
}

func TestFlakyReader_NoZeroReadLoop(t *testing.T) {
	cfg := FlakyConfig{
		Size:   1024,
		FailAt: []int64{0},
	}

	r := NewFlakyReader(cfg)

	buf := make([]byte, 128)

	_, err := r.Read(buf)
	require.Error(t, err)
}

func TestFlakyReader_ResumeAfterFailure(t *testing.T) {
	cfg := FlakyConfig{
		Size:           1024,
		FailAt:         []int64{512},
		RepeatFailures: false,
	}

	r := NewFlakyReader(cfg)

	buf := make([]byte, 256)

	var total int64
	failed := false

	for {
		n, err := r.Read(buf)
		total += int64(n)

		if err == ErrFlaky {
			failed = true
			continue
		}

		if err == io.EOF {
			break
		}
	}

	require.True(t, failed)
	require.Equal(t, int64(1024), total)
}
