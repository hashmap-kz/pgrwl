package ioutils

import (
	"fmt"
	"io"
)

// MultiCloser wraps an io.Reader and a set of io.Closers.
// Useful when wrapping readers but needing to close underlying resources (e.g., file + decompress + decrypt).
type MultiCloser struct {
	io.Reader
	closers []io.Closer
}

// NewMultiCloser constructs a ReadCloser from a reader and multiple closers.
// Closers will be closed in order when Close is called.
func NewMultiCloser(reader io.Reader, closers ...io.Closer) io.ReadCloser {
	return &MultiCloser{
		Reader:  reader,
		closers: closers,
	}
}

func (r *MultiCloser) Close() error {
	var firstErr error
	seen := make(map[io.Closer]struct{})
	for _, closer := range r.closers {
		if _, ok := seen[closer]; ok {
			continue
		}
		seen[closer] = struct{}{}
		if err := closer.Close(); err != nil {
			if firstErr != nil {
				firstErr = fmt.Errorf("%w; %v", firstErr, err)
			} else {
				firstErr = err
			}
		}
	}
	return firstErr
}
