package readers

import "io"

type FakeLargeReader struct {
	remaining int64
}

func NewFakeLargeReader(size int64) *FakeLargeReader {
	return &FakeLargeReader{
		remaining: size,
	}
}

func (r *FakeLargeReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}

	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}

	// fill with deterministic fake data
	for i := range p {
		p[i] = 'A'
	}

	r.remaining -= int64(len(p))

	return len(p), nil
}
