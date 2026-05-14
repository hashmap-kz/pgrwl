//go:build integration_storage

package integration

import (
	"bytes"
	"context"
	"io"
	"testing"

	storage "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorage_BackupLayout_ListTopLevelBackupDirs(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			const backupA = "20260502070500"
			const backupB = "20260502071500"

			putStorageText(t, ctx, store, "backups/"+backupA+"/"+backupA+".json", `{"backup":"a"}`)
			putStorageText(t, ctx, store, "backups/"+backupA+"/base.tar", "base-a")
			putStorageText(t, ctx, store, "backups/"+backupA+"/25222.tar", "tablespace-a")

			putStorageText(t, ctx, store, "backups/"+backupB+"/"+backupB+".json", `{"backup":"b"}`)
			putStorageText(t, ctx, store, "backups/"+backupB+"/base.tar", "base-b")

			// Loose files below backups/ are not backup dirs.
			putStorageText(t, ctx, store, "backups/README.txt", "not a backup dir")

			// WAL archive is a sibling layout and must not appear when listing backups/.
			putStorageText(t, ctx, store, "wal-archive/000000010000003C000000D9", "wal")

			got, err := store.ListTopLevelDirs(ctx, "backups")
			require.NoError(t, err)

			assert.Equal(t, map[string]bool{
				"backups/" + backupA: true,
				"backups/" + backupB: true,
			}, got)
		})
	}
}

func TestStorage_BackupLayout_ListOneBackupDoesNotReturnSiblingPrefix(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			const backupID = "20260502070500"

			putStorageText(t, ctx, store, "backups/"+backupID+"/"+backupID+".json", `{"ok":true}`)
			putStorageText(t, ctx, store, "backups/"+backupID+"/base.tar", "base")
			putStorageText(t, ctx, store, "backups/"+backupID+"-old/base.tar", "must-not-leak")
			putStorageText(t, ctx, store, "backups/"+backupID+"0/base.tar", "must-not-leak-either")

			listed, err := store.List(ctx, "backups/"+backupID)
			require.NoError(t, err)

			assert.ElementsMatch(t, []string{
				"backups/" + backupID + "/" + backupID + ".json",
				"backups/" + backupID + "/base.tar",
			}, listed)
		})
	}
}

func TestStorage_BackupLayout_DeleteDirDeletesOnlySelectedBackup(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			const backupID = "20260502070500"

			targetFiles := []string{
				"backups/" + backupID + "/" + backupID + ".json",
				"backups/" + backupID + "/base.tar",
				"backups/" + backupID + "/25222.tar",
			}

			for _, p := range targetFiles {
				putStorageText(t, ctx, store, p, "delete-me")
			}

			mustRemain := []string{
				"backups/" + backupID + "-old/base.tar",
				"backups/" + backupID + "0/base.tar",
				"backups/20260502071500/base.tar",
				"wal-archive/000000010000003C000000D9",
			}

			for _, p := range mustRemain {
				putStorageText(t, ctx, store, p, "keep-me")
			}

			require.NoError(t, store.DeleteDir(ctx, "backups/"+backupID))

			for _, p := range targetFiles {
				assertStorageMissing(t, ctx, store, p)
			}

			for _, p := range mustRemain {
				assertStorageExists(t, ctx, store, p)
			}
		})
	}
}

func TestStorage_WALArchiveLayout_ListInfoRootObjectsAndNestedObjects(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			files := []string{
				"wal-archive/000000010000003C000000D8",
				"wal-archive/000000010000003C000000D9",
				"wal-archive/000000010000003C000000DA.partial",
				"wal-archive/00000002.history",
				"wal-archive/README.txt",
				"wal-archive/nested/not-a-root-wal",
			}

			for _, p := range files {
				putStorageText(t, ctx, store, p, p)
			}

			infos, err := store.List(ctx, "wal-archive")
			require.NoError(t, err)

			var paths []string
			for _, info := range infos {
				paths = append(paths, info.Path)
			}

			assert.ElementsMatch(t, files, paths)

			topDirs, err := store.ListTopLevelDirs(ctx, "wal-archive")
			require.NoError(t, err)

			assert.Equal(t, map[string]bool{
				"wal-archive/nested": true,
			}, topDirs)
		})
	}
}

func TestStorage_WALArchiveLayout_DeleteAllBulkDeletesExactWALsOnly(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			deleteMe := []string{
				"wal-archive/000000010000003C000000D8",
				"wal-archive/000000010000003C000000DA",
			}

			mustRemain := []string{
				// Same prefix as D8, but not the same logical WAL name.
				"wal-archive/000000010000003C000000D8.partial",
				"wal-archive/000000010000003C000000D80",

				// Newer WAL.
				"wal-archive/000000010000003C000000D9",

				// Timeline history and non-WAL metadata.
				"wal-archive/00000002.history",
				"wal-archive/README.txt",

				// Sibling storage area.
				"backups/20260502070500/base.tar",
			}

			for _, p := range deleteMe {
				putStorageText(t, ctx, store, p, "delete-me")
			}
			for _, p := range mustRemain {
				putStorageText(t, ctx, store, p, "keep-me")
			}

			require.NoError(t, deleteAllBulk(ctx, store, deleteMe))

			for _, p := range deleteMe {
				assertStorageMissing(t, ctx, store, p)
			}

			for _, p := range mustRemain {
				assertStorageExists(t, ctx, store, p)
			}
		})
	}
}

func TestStorage_BackupAndWALArchiveLayout_DoNotCrossDelete(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			const backupID = "20260502070500"

			backupFiles := []string{
				"backups/" + backupID + "/" + backupID + ".json",
				"backups/" + backupID + "/base.tar",
				"backups/" + backupID + "/25222.tar",
			}

			walFiles := []string{
				"wal-archive/000000010000003C000000D8",
				"wal-archive/000000010000003C000000D9",
				"wal-archive/000000010000003C000000DA.partial",
			}

			for _, p := range backupFiles {
				putStorageText(t, ctx, store, p, "backup")
			}
			for _, p := range walFiles {
				putStorageText(t, ctx, store, p, "wal")
			}

			require.NoError(t, store.DeleteDir(ctx, "backups/"+backupID))

			for _, p := range backupFiles {
				assertStorageMissing(t, ctx, store, p)
			}
			for _, p := range walFiles {
				assertStorageExists(t, ctx, store, p)
			}
		})
	}
}

func TestStorage_BasebackupArtifactsRoundTrip(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			const backupID = "20260502070500"

			objects := map[string]string{
				"backups/" + backupID + "/" + backupID + ".json": `{"started_at":"2026-05-02T07:05:00Z"}`,
				"backups/" + backupID + "/base.tar":              "base-tar-content",
				"backups/" + backupID + "/25222.tar":             "tablespace-25222-content",
				"backups/" + backupID + "/backup_manifest":       "pg-backup-manifest-content",
			}

			for p, content := range objects {
				putStorageText(t, ctx, store, p, content)
			}

			for p, want := range objects {
				assertStorageText(t, ctx, store, p, want)
			}

			listed, err := store.List(ctx, "backups/"+backupID)
			require.NoError(t, err)

			var wantPaths []string
			for p := range objects {
				wantPaths = append(wantPaths, p)
			}

			assert.ElementsMatch(t, wantPaths, fileInfoToStrList(listed))
		})
	}
}

func putStorageText(
	t *testing.T,
	ctx context.Context,
	store storage.Storage,
	path string,
	content string,
) {
	t.Helper()

	require.NoError(t, store.Put(ctx, path, bytes.NewReader([]byte(content))), "put %s", path)
}

func assertStorageExists(
	t *testing.T,
	ctx context.Context,
	store storage.Storage,
	path string,
) {
	t.Helper()

	exists, err := store.Exists(ctx, path)
	require.NoError(t, err, "exists %s", path)
	assert.True(t, exists, "%s should exist", path)
}

func assertStorageMissing(
	t *testing.T,
	ctx context.Context,
	store storage.Storage,
	path string,
) {
	t.Helper()

	exists, err := store.Exists(ctx, path)
	require.NoError(t, err, "exists %s", path)
	assert.False(t, exists, "%s should be missing", path)
}

func assertStorageText(
	t *testing.T,
	ctx context.Context,
	store storage.Storage,
	path string,
	want string,
) {
	t.Helper()

	r, err := store.Get(ctx, path)
	require.NoError(t, err, "get %s", path)

	data, err := io.ReadAll(r)
	require.NoError(t, err, "read %s", path)
	require.NoError(t, r.Close(), "close %s", path)

	assert.Equal(t, want, string(data), path)
}
