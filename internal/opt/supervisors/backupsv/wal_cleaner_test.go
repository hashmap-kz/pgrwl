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
	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(context.Background(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "keepFromWAL is empty")
}

func TestWALCleanerDeleteBeforeDeletesOnlyWalBeforeBoundary(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	putRawObject(t, backend, "000000010000003C000000D8")
	putRawObject(t, backend, "000000010000003C000000D9")
	putRawObject(t, backend, "000000010000003C000000DA")
	putRawObject(t, backend, "000000010000003C000000DB.gz")
	putRawObject(t, backend, "000000010000003C000000DC.gz.aes")
	putRawObject(t, backend, "00000002.history")
	putRawObject(t, backend, "README.txt")

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(ctx, "000000010000003C000000DA")

	require.NoError(t, err)

	for _, deleted := range []string{
		"000000010000003C000000D8",
		"000000010000003C000000D9",
	} {
		exists, err := backend.Exists(ctx, deleted)
		require.NoError(t, err)
		assert.False(t, exists, "expected %s to be deleted", deleted)
	}

	for _, kept := range []string{
		"000000010000003C000000DA",
		"000000010000003C000000DB.gz",
		"000000010000003C000000DC.gz.aes",
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

	putRawObject(t, backend, "000000010000003C000000D8.gz")
	putRawObject(t, backend, "000000010000003C000000D9.zst.aes")
	putRawObject(t, backend, "000000010000003C000000DA.lz4")

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(ctx, "000000010000003C000000DA")

	require.NoError(t, err)

	for _, deleted := range []string{
		"000000010000003C000000D8.gz",
		"000000010000003C000000D9.zst.aes",
	} {
		exists, err := backend.Exists(ctx, deleted)
		require.NoError(t, err)
		assert.False(t, exists, "expected %s to be deleted", deleted)
	}

	exists, err := backend.Exists(ctx, "000000010000003C000000DA.lz4")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestWALCleanerDeleteBeforeReturnsContextError(t *testing.T) {
	backend := st.NewInMemoryStorage()
	putRawObject(t, backend, "000000010000003C000000D8")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(ctx, "000000010000003C000000D9")

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
	putRawObject(t, backend, "000000010000003C000000D8")

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(context.Background(), "000000010000003C000000D9")

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
	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(context.Background(), "000000010000003C000000D9")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "list WAL archive")
}

func TestWALCleanerDeleteBeforeIgnoresNestedPathBaseName(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	putRawObject(t, backend, "archive/000000010000003C000000D8")
	putRawObject(t, backend, "archive/000000010000003C000000DA")

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})

	err := cleaner.DeleteBefore(ctx, "000000010000003C000000DA")

	require.NoError(t, err)

	oldExists, err := backend.Exists(ctx, "archive/000000010000003C000000D8")
	require.NoError(t, err)
	assert.True(t, oldExists)

	boundaryExists, err := backend.Exists(ctx, "archive/000000010000003C000000DA")
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
	require.NoError(t, backend.Put(ctx, "000000010000003C000000D8.gz.aes", strings.NewReader("x")))

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})
	err := cleaner.DeleteBefore(ctx, "000000010000003C000000D9")
	require.NoError(t, err)

	_, err = backend.Get(ctx, "000000010000003C000000D8.gz.aes")
	assert.Error(t, err)
}

func TestWALCleanerDeleteBeforeMatchesConfiguredWalArchiveSubpath(t *testing.T) {
	ctx := context.Background()
	backend := st.NewInMemoryStorage()

	// Production storage is already scoped to the wal-archive subpath.
	// So the cleaner sees only WAL archive object names, not root receive-dir files
	// such as 000000010000003C000000DC.partial or backups/<id>/*.
	putRawObject(t, backend, "000000010000003C000000D8")
	putRawObject(t, backend, "000000010000003C000000D9")
	putRawObject(t, backend, "000000010000003C000000DA")
	putRawObject(t, backend, "000000010000003C000000DB")

	cleaner := NewWALCleaner(&BackupSupervisorOpts{WalStor: newPlainVariadicStorage(t, backend)})
	err := cleaner.DeleteBefore(ctx, "000000010000003C000000DA")
	require.NoError(t, err)

	for _, deleted := range []string{
		"000000010000003C000000D8",
		"000000010000003C000000D9",
	} {
		exists, err := backend.Exists(ctx, deleted)
		require.NoError(t, err)
		assert.False(t, exists, "expected %s to be deleted", deleted)
	}

	for _, kept := range []string{
		"000000010000003C000000DA",
		"000000010000003C000000DB",
	} {
		exists, err := backend.Exists(ctx, kept)
		require.NoError(t, err)
		assert.True(t, exists, "expected %s to be kept", kept)
	}
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
