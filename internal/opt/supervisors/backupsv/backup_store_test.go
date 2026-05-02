package backupsv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStorage struct {
	files map[string][]byte

	listTopLevelDirsErr error
	deleteDirErr        error
	getErr              error
	deletedDirs         []string
}

var _ st.Storage = (*testStorage)(nil)

func newTestStorage() *testStorage {
	return &testStorage{files: make(map[string][]byte)}
}

func (s *testStorage) Put(_ context.Context, path string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.files[path] = data
	return nil
}

func (s *testStorage) Get(_ context.Context, path string) (io.ReadCloser, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	data, ok := s.files[path]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *testStorage) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	for path := range s.files {
		if strings.HasPrefix(path, prefix) {
			out = append(out, path)
		}
	}
	return out, nil
}

func (s *testStorage) ListInfo(ctx context.Context, prefix string) ([]st.FileInfo, error) {
	files, err := s.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	infos := make([]st.FileInfo, 0, len(files))
	for _, path := range files {
		infos = append(infos, st.FileInfo{Path: path, Size: int64(len(s.files[path]))})
	}
	return infos, nil
}

func (s *testStorage) Delete(_ context.Context, path string) error {
	delete(s.files, path)
	return nil
}

func (s *testStorage) DeleteAll(_ context.Context, path string) error {
	prefix := strings.TrimSuffix(path, "/")
	if prefix != "" {
		prefix += "/"
	}
	for key := range s.files {
		if key == path || strings.HasPrefix(key, prefix) {
			delete(s.files, key)
		}
	}
	return nil
}

func (s *testStorage) DeleteDir(ctx context.Context, path string) error {
	if s.deleteDirErr != nil {
		return s.deleteDirErr
	}
	s.deletedDirs = append(s.deletedDirs, path)
	return s.DeleteAll(ctx, path)
}

func (s *testStorage) DeleteAllBulk(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := s.DeleteAll(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (s *testStorage) Exists(_ context.Context, path string) (bool, error) {
	_, ok := s.files[path]
	return ok, nil
}

func (s *testStorage) ListTopLevelDirs(ctx context.Context, prefix string) (map[string]bool, error) {
	if s.listTopLevelDirsErr != nil {
		return nil, s.listTopLevelDirsErr
	}
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}

	out := make(map[string]bool)
	for path := range s.files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(path, prefix)
		idx := strings.Index(rel, "/")
		if idx <= 0 {
			continue
		}
		out[rel[:idx]] = true
	}
	return out, nil
}

func (s *testStorage) Rename(_ context.Context, oldPath, newPath string) error {
	data, ok := s.files[oldPath]
	if !ok {
		return errors.New("not found")
	}
	s.files[newPath] = data
	delete(s.files, oldPath)
	return nil
}

//nolint:gocritic
func putManifest(t *testing.T, storage st.Storage, backupID string, result backupdto.Result) {
	t.Helper()

	data, err := json.Marshal(result)
	require.NoError(t, err)

	path := backupID + "/" + backupID + ".json"
	require.NoError(t, storage.Put(context.Background(), path, bytes.NewReader(data)))
}

func testBackupResult(startedAt string) backupdto.Result {
	started, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		panic(err)
	}
	return backupdto.Result{
		StartedAt:  started,
		FinishedAt: started.Add(time.Minute),
		StartLSN:   pglogrepl.LSN(0x1000000),
		TimelineID: 1,
		BytesTotal: 1234,
	}
}

func TestBackupStoreListBackupDirs(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	putManifest(t, storage, "F20260429T120000-root", testBackupResult("2026-04-29T12:00:00Z"))
	putManifest(t, storage, "I20260429T130000-F20260429T120000", testBackupResult("2026-04-29T13:00:00Z"))
	require.NoError(t, storage.Put(ctx, "loose-file.txt", strings.NewReader("ignored")))

	dirs, err := store.ListBackupDirs(ctx)

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{
		"F20260429T120000-root":             true,
		"I20260429T130000-F20260429T120000": true,
	}, dirs)
}

func TestBackupStoreReadManifest(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	putManifest(t, storage, "F20260429T120000-root", testBackupResult("2026-04-29T12:00:00Z"))

	got, err := store.ReadManifest(ctx, "F20260429T120000-root")

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int32(1), got.TimelineID)
	assert.Equal(t, int64(1234), got.BytesTotal)
}

func TestBackupStoreDeleteBackupsDeletesOnlyRequestedTopLevelDirs(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	putManifest(t, storage, "F20260429T120000-root", testBackupResult("2026-04-29T12:00:00Z"))
	putManifest(t, storage, "I20260429T130000-F20260429T120000", testBackupResult("2026-04-29T13:00:00Z"))
	putManifest(t, storage, "I20260429T140000-I20260429T130000", testBackupResult("2026-04-29T14:00:00Z"))

	err := store.DeleteBackups(ctx, []string{
		"F20260429T120000-root",
		"I20260429T130000-F20260429T120000",
	})

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"F20260429T120000-root",
		"I20260429T130000-F20260429T120000",
	}, storage.deletedDirs)

	exists, err := storage.Exists(ctx, "I20260429T140000-I20260429T130000/I20260429T140000-I20260429T130000.json")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestBackupStoreDeleteBackupsPropagatesDeleteError(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	storage.deleteDirErr = errors.New("delete failed")
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	putManifest(t, storage, "F20260429T120000-root", testBackupResult("2026-04-29T12:00:00Z"))

	err := store.DeleteBackups(ctx, []string{"F20260429T120000-root"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete backup")
}

func TestBackupStoreDeleteBackupsAllowsUnreadableManifest(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	require.NoError(t, storage.Put(ctx, "F20260429T120000-root/F20260429T120000-root.json", strings.NewReader("not-json")))

	err := store.DeleteBackups(ctx, []string{"F20260429T120000-root"})

	require.NoError(t, err)
	assert.Equal(t, []string{"F20260429T120000-root"}, storage.deletedDirs)
}

func TestBackupStoreDeleteBackupsEmptyInputDoesNothing(t *testing.T) {
	ctx := context.Background()
	storage := newTestStorage()
	store := NewBackupStore(&config.Config{}, testLogger(), storage)

	err := store.DeleteBackups(ctx, nil)

	require.NoError(t, err)
	assert.Empty(t, storage.deletedDirs)
}
