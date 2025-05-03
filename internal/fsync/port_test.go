package fsync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFsyncFname_Success(t *testing.T) {
	tmp := t.TempDir()
	fpath := filepath.Join(tmp, "testfile.txt")

	err := os.WriteFile(fpath, []byte("data"), 0o600)
	require.NoError(t, err)

	err = FsyncFname(fpath)
	require.NoError(t, err)
}

func TestFsyncFname_FileNotExist(t *testing.T) {
	err := FsyncFname("/nonexistent/path/file.txt")
	require.Error(t, err)
}

func TestFsyncDir_Success(t *testing.T) {
	tmp := t.TempDir()
	err := FsyncDir(tmp)
	require.NoError(t, err)
}

func TestFsyncDir_DirNotExist(t *testing.T) {
	err := FsyncDir("/nonexistent/dir")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot open dir")
}

func TestFsyncFnameAndDir_Success(t *testing.T) {
	tmp := t.TempDir()
	fpath := filepath.Join(tmp, "file.txt")
	err := os.WriteFile(fpath, []byte("test"), 0o600)
	require.NoError(t, err)

	err = FsyncFnameAndDir(fpath)
	require.NoError(t, err)
}

func TestFsyncFnameAndDir_FileMissing(t *testing.T) {
	tmp := t.TempDir()
	fpath := filepath.Join(tmp, "missing.txt")

	err := FsyncFnameAndDir(fpath)
	require.Error(t, err)
}

func TestFsyncFnameAndDir_DirMissing(t *testing.T) {
	fpath := "/nonexistent/missing.txt"

	err := FsyncFnameAndDir(fpath)
	require.Error(t, err)
}
