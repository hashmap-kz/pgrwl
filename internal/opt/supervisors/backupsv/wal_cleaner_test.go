package backupsv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPlainVariadicStorage(t *testing.T, backend st.Storage) *st.VariadicStorage {
	t.Helper()

	vs, err := st.NewVariadicStorage(backend, st.Algorithms{}, "")
	require.NoError(t, err)
	return vs
}

func putRawObject(t *testing.T, backend st.Storage, path string) {
	t.Helper()
	require.NoError(t, backend.Put(context.Background(), path, bytes.NewReader([]byte("x"))))
}

func TestWALCleanerDeleteBeforeRejectsEmptyBoundary(t *testing.T) {
	backend := st.NewInMemoryStorage()
	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(context.Background(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keepFromWAL is empty")
}

func TestWALCleanerDeleteBeforeDeletesOnlyWalBeforeBoundary(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	putRawObject(t, backend, "000000010000000000000001")
	putRawObject(t, backend, "000000010000000000000002")
	putRawObject(t, backend, "000000010000000000000003")
	putRawObject(t, backend, "000000010000000000000004.gz")
	putRawObject(t, backend, "000000010000000000000005.gz.aes")
	putRawObject(t, backend, "00000002.history")
	putRawObject(t, backend, "README.txt")

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(ctx, "000000010000000000000003")

	require.NoError(t, err)

	for _, deleted := range []string{
		"000000010000000000000001",
		"000000010000000000000002",
	} {
		exists, err := backend.Exists(ctx, deleted)
		require.NoError(t, err)
		assert.False(t, exists, "expected %s to be deleted", deleted)
	}

	for _, kept := range []string{
		"000000010000000000000003",
		"000000010000000000000004.gz",
		"000000010000000000000005.gz.aes",
		"00000002.history",
		"README.txt",
	} {
		exists, err := backend.Exists(ctx, kept)
		require.NoError(t, err)
		assert.True(t, exists, "expected %s to be kept", kept)
	}
}

func TestWALCleanerDeleteBeforeHandlesCompressedOldWal(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	putRawObject(t, backend, "000000010000000000000001.gz")
	putRawObject(t, backend, "000000010000000000000002.zst.aes")
	putRawObject(t, backend, "000000010000000000000003.lz4")

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(ctx, "000000010000000000000003")

	require.NoError(t, err)

	for _, deleted := range []string{
		"000000010000000000000001.gz",
		"000000010000000000000002.zst.aes",
	} {
		exists, err := backend.Exists(ctx, deleted)
		require.NoError(t, err)
		assert.False(t, exists, "expected %s to be deleted", deleted)
	}

	exists, err := backend.Exists(ctx, "000000010000000000000003.lz4")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestWALCleanerDeleteBeforeReturnsContextError(t *testing.T) {
	backend := st.NewInMemoryStorage()
	putRawObject(t, backend, "000000010000000000000001")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(ctx, "000000010000000000000002")

	assert.ErrorIs(t, err, context.Canceled)
}

type deleteFailStorage struct {
	*st.InMemoryStorage
}

func (s *deleteFailStorage) Delete(_ context.Context, _ string) error {
	return errors.New("delete failed")
}

func TestWALCleanerDeleteBeforePropagatesDeleteError(t *testing.T) {
	backend := &deleteFailStorage{InMemoryStorage: st.NewInMemoryStorage()}
	putRawObject(t, backend, "000000010000000000000001")

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(context.Background(), "000000010000000000000002")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete WAL")
}

type listFailStorage struct {
	*st.InMemoryStorage
}

func (s *listFailStorage) ListInfo(_ context.Context, _ string) ([]st.FileInfo, error) {
	return nil, errors.New("list failed")
}

func TestWALCleanerDeleteBeforePropagatesListError(t *testing.T) {
	backend := &listFailStorage{InMemoryStorage: st.NewInMemoryStorage()}
	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(context.Background(), "000000010000000000000002")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list WAL archive")
}

func TestWALCleanerDeleteBeforeIgnoresNestedPathBaseName(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	putRawObject(t, backend, "archive/000000010000000000000001")
	putRawObject(t, backend, "archive/000000010000000000000003")

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))

	err := cleaner.DeleteBefore(ctx, "000000010000000000000003")

	require.NoError(t, err)

	oldExists, err := backend.Exists(ctx, "archive/000000010000000000000001")
	require.NoError(t, err)
	assert.False(t, oldExists)

	boundaryExists, err := backend.Exists(ctx, "archive/000000010000000000000003")
	require.NoError(t, err)
	assert.True(t, boundaryExists)
}

// Compile-time guard that the failing wrappers still implement Storage.
var (
	_ st.Storage = (*deleteFailStorage)(nil)
	_ st.Storage = (*listFailStorage)(nil)
)

func TestWALCleanerDeleteBeforeWithRawPathStillDeletesRawObject(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	// This test protects the current walCleaner behavior: it receives raw paths
	// from ListInfoRaw and passes those same raw paths to Delete.
	require.NoError(t, backend.Put(ctx, "000000010000000000000001.gz.aes", strings.NewReader("x")))

	cleaner := NewWALCleaner(&Opts{}, testLogger(), newPlainVariadicStorage(t, backend))
	err := cleaner.DeleteBefore(ctx, "000000010000000000000002")
	require.NoError(t, err)

	_, err = backend.Get(ctx, "000000010000000000000001.gz.aes")
	assert.Error(t, err)
}

func TestPutRawObjectHelperUsesReader(t *testing.T) {
	backend := st.NewInMemoryStorage()
	putRawObject(t, backend, "x")
	r, err := backend.Get(context.Background(), "x")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "x", string(data))
}
