package fakereaders

import "io"

type RetryableReader struct {
	factory func() io.Reader
	current io.Reader
}

func NewRetryableReader(factory func() io.Reader) *RetryableReader {
	return &RetryableReader{
		factory: factory,
		current: factory(),
	}
}

func (r *RetryableReader) Read(p []byte) (int, error) {
	n, err := r.current.Read(p)

	if err != nil {
		if n == 0 {
			// safe to retry only if nothing was consumed
			r.current = r.factory()
		}
	}

	return n, err
}
