package fakereaders

import "io"

type RetryableReader struct {
	factory    func() io.Reader
	current    io.Reader
	bytesRead  int64
	maxRetries int
}

func NewRetryableReader(factory func() io.Reader, maxRetries int) *RetryableReader {
	return &RetryableReader{
		factory:    factory,
		current:    factory(),
		maxRetries: maxRetries,
	}
}

func (r *RetryableReader) Read(p []byte) (int, error) {
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		n, err := r.current.Read(p)
		r.bytesRead += int64(n)

		if err != nil && n == 0 && r.bytesRead == 0 {
			if attempt == r.maxRetries {
				return 0, err
			}
			r.current = r.factory()
			continue
		}

		return n, err
	}
	panic("unreachable")
}
