package mock

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
