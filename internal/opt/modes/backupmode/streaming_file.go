package backupmode

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	st "github.com/hashmap-kz/storecrypt/pkg/storage"
)

type StreamingFile struct {
	path   string
	pw     *io.PipeWriter
	done   chan struct{}
	putErr error
	log    *slog.Logger
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
			log.Info("closing", slog.String("file", path))
			close(sf.done)
		}()
		sf.putErr = storage.Put(ctx, path, pr)
	}()

	return sf
}

func (sf *StreamingFile) Write(p []byte) (int, error) {
	return sf.pw.Write(p)
}

func (sf *StreamingFile) Close() error {
	if sf == nil {
		return nil
	}
	_ = sf.pw.Close()
	<-sf.done
	if sf.putErr != nil {
		return fmt.Errorf("storage put failed for %s: %w", sf.path, sf.putErr)
	}
	return nil
}
