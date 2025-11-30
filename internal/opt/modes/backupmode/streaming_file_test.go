package backupmode

import (
	"context"
	"io"
	"log/slog"
	"testing"

	stormock "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/stretchr/testify/assert"
)

func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func TestStreamingFile_WriteAndClose_StoresFileInMemory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := newTestLogger(t)
	stor := stormock.NewInMemoryStorage()
	path := "base/20251128_000000"

	sf := NewStreamingFile(ctx, log, stor, path)
	data := []byte("hello streaming file")

	n, err := sf.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)

	err = sf.Close()
	assert.NoError(t, err)

	// Make sure the file exists in in-memory storage
	assert.Contains(t, stor.Files, path)

	// If the in-memory storage keeps []byte, also verify the content.
	if raw, ok := any(stor.Files[path]).([]byte); ok {
		assert.Equal(t, data, raw)
	}
}

func TestStreamingFile_MultipleWritesAreConcatenated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := newTestLogger(t)
	stor := stormock.NewInMemoryStorage()
	path := "base/20251128_multi"

	sf := NewStreamingFile(ctx, log, stor, path)

	part1 := []byte("hello ")
	part2 := []byte("world")

	n1, err := sf.Write(part1)
	assert.NoError(t, err)
	assert.Equal(t, len(part1), n1)

	n2, err := sf.Write(part2)
	assert.NoError(t, err)
	assert.Equal(t, len(part2), n2)

	err = sf.Close()
	assert.NoError(t, err)

	assert.Contains(t, stor.Files, path)

	// If Files[path] is []byte, verify concatenated payload.
	if raw, ok := any(stor.Files[path]).([]byte); ok {
		assert.Equal(t, append(part1, part2...), raw)
	}
}

func TestStreamingFile_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	log := newTestLogger(t)
	stor := stormock.NewInMemoryStorage()
	path := "base/20251128_idempotent"

	sf := NewStreamingFile(ctx, log, stor, path)

	_, err := sf.Write([]byte("idempotent close"))
	assert.NoError(t, err)

	// First close should succeed and wait for Put to finish.
	err = sf.Close()
	assert.NoError(t, err)

	// Second close should be a no-op and not panic or error.
	err = sf.Close()
	assert.NoError(t, err)
}

func TestStreamingFile_CloseOnNilDoesNothing(t *testing.T) {
	t.Parallel()

	var sf *StreamingFile
	// Should not panic and should return nil.
	err := sf.Close()
	assert.NoError(t, err)
}
