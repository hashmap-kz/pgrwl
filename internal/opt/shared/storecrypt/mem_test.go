package storecrypt

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryStorage_PutAndGet(t *testing.T) {
	s := NewInMemoryStorage()
	ctx := context.Background()

	err := s.Put(ctx, "test/file1", bytes.NewBufferString("hello"))
	assert.NoError(t, err)

	r, err := s.Get(ctx, "test/file1")
	assert.NoError(t, err)
	defer r.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(r)
	assert.NoError(t, err)
	assert.Equal(t, "hello", buf.String())
}

func TestInMemoryStorage_Exists(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	exists, err := s.Exists(ctx, "missing.txt")
	assert.NoError(t, err)
	assert.False(t, exists)

	err = s.Put(ctx, "file.txt", bytes.NewReader([]byte("data")))
	assert.NoError(t, err)
	exists, err = s.Exists(ctx, "file.txt")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestInMemoryStorage_Delete(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	err := s.Put(ctx, "file.txt", bytes.NewReader([]byte("data")))
	assert.NoError(t, err)
	err = s.Delete(ctx, "file.txt")
	assert.NoError(t, err)

	_, err = s.Get(ctx, "file.txt")
	assert.Error(t, err)
}

func TestInMemoryStorage_DeleteAll(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	err := s.Put(ctx, "dir/file1", bytes.NewReader([]byte("1")))
	assert.NoError(t, err)
	err = s.Put(ctx, "dir/file2", bytes.NewReader([]byte("2")))
	assert.NoError(t, err)
	err = s.Put(ctx, "other/file3", bytes.NewReader([]byte("3")))
	assert.NoError(t, err)

	err = s.DeleteAll(ctx, "dir")
	assert.NoError(t, err)

	_, err = s.Get(ctx, "dir/file1")
	assert.Error(t, err)
	_, err = s.Get(ctx, "other/file3")
	assert.NoError(t, err)
}

func TestInMemoryStorage_List(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	// Populate storage with files in different "directories"
	err := s.Put(ctx, "dir1/file1.txt", strings.NewReader("data1"))
	assert.NoError(t, err)

	err = s.Put(ctx, "dir1/file2.txt", strings.NewReader("data2"))
	assert.NoError(t, err)
	err = s.Put(ctx, "dir2/file3.txt", strings.NewReader("data3"))
	assert.NoError(t, err)
	err = s.Put(ctx, "dir1/subdir/file4.txt", strings.NewReader("data4"))
	assert.NoError(t, err)

	// List files under dir1
	files, err := s.List(ctx, "dir1")
	assert.NoError(t, err)

	// We expect file1.txt, file2.txt, and subdir/file4.txt under dir1
	expected := map[string]bool{
		"dir1/file1.txt":        true,
		"dir1/file2.txt":        true,
		"dir1/subdir/file4.txt": true,
	}
	assert.Len(t, files, 3)

	for _, file := range files {
		assert.True(t, expected[file], "unexpected file listed: %s", file)
	}
}

func TestInMemoryStorage_ListInfo(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	err := s.Put(ctx, "a/b/c.txt", bytes.NewReader([]byte("content")))
	assert.NoError(t, err)
	err = s.Put(ctx, "a/b/d.txt", bytes.NewReader([]byte("another")))
	assert.NoError(t, err)
	infos, err := s.ListInfo(ctx, "a/b")
	assert.NoError(t, err)
	assert.Len(t, infos, 2)
}

func TestInMemoryStorage_ListTopLevelDirs(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	err := s.Put(ctx, "prefix/dir1/file1.txt", strings.NewReader("data1"))
	assert.NoError(t, err)
	err = s.Put(ctx, "prefix/dir1/subdir/file2.txt", strings.NewReader("data2"))
	assert.NoError(t, err)
	err = s.Put(ctx, "prefix/dir2/file3.txt", strings.NewReader("data3"))
	assert.NoError(t, err)
	err = s.Put(ctx, "prefix/dir3/nested/file4.txt", strings.NewReader("data4"))
	assert.NoError(t, err)
	err = s.Put(ctx, "prefix/file5.txt", strings.NewReader("data5"))
	assert.NoError(t, err)
	err = s.Put(ctx, "other/dir4/file6.txt", strings.NewReader("data6"))
	assert.NoError(t, err)

	result, err := s.ListTopLevelDirs(ctx, "prefix")
	assert.NoError(t, err)

	expected := map[string]bool{
		"dir1": true,
		"dir2": true,
		"dir3": true,
	}

	assert.Len(t, result, 3)
	for dir := range result {
		assert.True(t, expected[dir], "unexpected directory: %s", dir)
	}

	result2, err := s.ListTopLevelDirs(ctx, "prefix/")
	assert.NoError(t, err)
	assert.Equal(t, result, result2)

	result3, err := s.ListTopLevelDirs(ctx, "nonexistent")
	assert.NoError(t, err)
	assert.Empty(t, result3)
}

// New tests below

func TestInMemoryStorage_DeleteDir(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	// Files under dir/ and elsewhere
	assert.NoError(t, s.Put(ctx, "dir/file1.txt", strings.NewReader("data1")))
	assert.NoError(t, s.Put(ctx, "dir/sub/file2.txt", strings.NewReader("data2")))
	assert.NoError(t, s.Put(ctx, "other/file3.txt", strings.NewReader("data3")))

	// Delete directory by prefix without trailing slash
	err := s.DeleteDir(ctx, "dir")
	assert.NoError(t, err)

	// All under "dir/" should be gone
	_, err = s.Get(ctx, "dir/file1.txt")
	assert.Error(t, err)
	_, err = s.Get(ctx, "dir/sub/file2.txt")
	assert.Error(t, err)

	// Other paths untouched
	_, err = s.Get(ctx, "other/file3.txt")
	assert.NoError(t, err)

	// Also make sure calling with trailing slash behaves the same
	assert.NoError(t, s.Put(ctx, "dir2/file4.txt", strings.NewReader("data4")))
	assert.NoError(t, s.Put(ctx, "dir2/sub/file5.txt", strings.NewReader("data5")))

	err = s.DeleteDir(ctx, "dir2/")
	assert.NoError(t, err)

	_, err = s.Get(ctx, "dir2/file4.txt")
	assert.Error(t, err)
	_, err = s.Get(ctx, "dir2/sub/file5.txt")
	assert.Error(t, err)
}

func TestInMemoryStorage_DeleteAllBulk(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	// Prepare several directories
	assert.NoError(t, s.Put(ctx, "a/file1.txt", strings.NewReader("a1")))
	assert.NoError(t, s.Put(ctx, "a/file2.txt", strings.NewReader("a2")))
	assert.NoError(t, s.Put(ctx, "b/file3.txt", strings.NewReader("b1")))
	assert.NoError(t, s.Put(ctx, "c/file4.txt", strings.NewReader("c1")))

	// Bulk delete "a" and "b"
	err := s.DeleteAllBulk(ctx, []string{"a", "b"})
	assert.NoError(t, err)

	// a/* and b/* should be gone
	_, err = s.Get(ctx, "a/file1.txt")
	assert.Error(t, err)
	_, err = s.Get(ctx, "a/file2.txt")
	assert.Error(t, err)
	_, err = s.Get(ctx, "b/file3.txt")
	assert.Error(t, err)

	// c/* should still exist
	_, err = s.Get(ctx, "c/file4.txt")
	assert.NoError(t, err)
}

func TestInMemoryStorage_GetNonExisting(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	_, err := s.Get(ctx, "nope.txt")
	assert.Error(t, err)
}

func TestInMemoryStorage_DeleteNonExisting(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	err := s.Delete(ctx, "nope.txt")
	assert.Error(t, err)
}
