package fakereaders

import (
	"errors"
	"io"
	"time"
)

var ErrFlaky = errors.New("fakereader: simulated failure")

type FlakyConfig struct {
	Size int64

	// Fail exactly at these byte offsets (sorted or unsorted - your choice)
	FailAt []int64

	// Optional delay per Read (simulate slow network)
	Delay time.Duration

	// If true → after failure, reader continues (multi-failure)
	// If false → stops after first failure
	RepeatFailures bool
}

type FlakyReader struct {
	cfg   FlakyConfig
	pos   int64
	failI int
}

func NewFlakyReader(cfg FlakyConfig) *FlakyReader {
	return &FlakyReader{cfg: cfg}
}

func (r *FlakyReader) Read(p []byte) (int, error) {
	if r.pos >= r.cfg.Size {
		return 0, io.EOF
	}

	if r.cfg.Delay > 0 {
		time.Sleep(r.cfg.Delay)
	}

	// next failure point
	var failAt int64 = -1
	if r.failI < len(r.cfg.FailAt) {
		failAt = r.cfg.FailAt[r.failI]
	}

	remaining := r.cfg.Size - r.pos
	n := len(p)

	if int64(n) > remaining {
		n = int(remaining)
	}

	// clamp to failure boundary (CRITICAL)
	if failAt >= 0 && r.pos < failAt && r.pos+int64(n) > failAt {
		n = int(failAt - r.pos)
	}

	// trigger failure exactly at boundary
	if failAt >= 0 && r.pos == failAt {
		if !r.cfg.RepeatFailures {
			r.failI = len(r.cfg.FailAt)
		} else {
			r.failI++
		}
		return 0, ErrFlaky
	}

	// fill deterministic data
	for i := 0; i < n; i++ {
		p[i] = byte((r.pos + int64(i)) % 251)
	}

	r.pos += int64(n)
	return n, nil
}
