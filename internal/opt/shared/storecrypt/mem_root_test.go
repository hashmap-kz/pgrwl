package storecrypt

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryStorageRootPrefixOperations(t *testing.T) {
	ctx := context.Background()
	s := NewInMemoryStorage()

	require.NoError(t, s.Put(ctx, "dir1/file1.txt", strings.NewReader("1")))
	require.NoError(t, s.Put(ctx, "dir2/file2.txt", strings.NewReader("2")))
	require.NoError(t, s.Put(ctx, "loose.txt", strings.NewReader("3")))

	files, err := s.List(ctx, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{
		"dir1/file1.txt",
		"dir2/file2.txt",
		"loose.txt",
	}, files)

	infos, err := s.ListInfo(ctx, "")
	require.NoError(t, err)
	assert.Len(t, infos, 3)

	dirs, err := s.ListTopLevelDirs(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{
		"dir1": true,
		"dir2": true,
	}, dirs)
}
