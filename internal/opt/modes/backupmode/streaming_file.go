package backupmode

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"
)

type StreamingFile struct {
	path string
	pw   *io.PipeWriter

	done chan struct{}
	log  *slog.Logger

	mu     sync.Mutex
	putErr error
	closed bool
}

func NewStreamingFile(ctx context.Context, log *slog.Logger, storage st.Storage, path string) *StreamingFile {
	pr, pw := io.Pipe()

	sf := &StreamingFile{
		path: path,
		pw:   pw,
		done: make(chan struct{}),
		log:  log,
	}

	go func() {
		defer func() {
			_ = pr.Close()
			log.Info("closing", slog.String("file", path))
			close(sf.done)
		}()

		err := storage.Put(ctx, path, pr)

		sf.mu.Lock()
		sf.putErr = err
		sf.mu.Unlock()

		if err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	return sf
}

func (sf *StreamingFile) Write(p []byte) (int, error) {
	sf.mu.Lock()
	closed := sf.closed
	sf.mu.Unlock()

	if closed {
		return 0, fmt.Errorf("write to closed streaming file: %s", sf.path)
	}

	return sf.pw.Write(p)
}

func (sf *StreamingFile) Close() error {
	if sf == nil {
		return nil
	}

	sf.mu.Lock()
	if sf.closed {
		sf.mu.Unlock()
		return nil
	}
	sf.closed = true
	sf.mu.Unlock()

	if err := sf.pw.Close(); err != nil {
		return err
	}

	<-sf.done

	sf.mu.Lock()
	defer sf.mu.Unlock()

	if sf.putErr != nil {
		return fmt.Errorf("storage put failed for %s: %w", sf.path, sf.putErr)
	}
	return nil
}
