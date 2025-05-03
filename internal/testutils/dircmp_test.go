package testutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareDirs_Identical(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeFile(t, dirA, "a.txt", "same content")
	writeFile(t, dirB, "a.txt", "same content")

	diffs, err := CompareDirs(dirA, dirB)
	assert.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestCompareDirs_MissingInB(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeFile(t, dirA, "only-in-a.txt", "content")

	diffs, err := CompareDirs(dirA, dirB)
	assert.NoError(t, err)
	assert.Len(t, diffs, 1)
	assert.Equal(t, "only-in-a.txt", diffs[0].Path)
	assert.True(t, diffs[0].MissingInB)
	assert.False(t, diffs[0].MissingInA)
}

func TestCompareDirs_MissingInA(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeFile(t, dirB, "only-in-b.txt", "content")

	diffs, err := CompareDirs(dirA, dirB)
	assert.NoError(t, err)
	assert.Len(t, diffs, 1)
	assert.Equal(t, "only-in-b.txt", diffs[0].Path)
	assert.True(t, diffs[0].MissingInA)
	assert.False(t, diffs[0].MissingInB)
}

func TestCompareDirs_DifferentContent(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeFile(t, dirA, "same-name.txt", "content-a")
	writeFile(t, dirB, "same-name.txt", "content-b")

	diffs, err := CompareDirs(dirA, dirB)
	assert.NoError(t, err)
	assert.Len(t, diffs, 1)
	assert.Equal(t, "same-name.txt", diffs[0].Path)
	assert.True(t, diffs[0].Different)
	assert.False(t, diffs[0].MissingInA)
	assert.False(t, diffs[0].MissingInB)
}

// Helper
func writeFile(t *testing.T, dir, name, content string) {
	fullPath := filepath.Join(dir, name)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	assert.NoError(t, err)
	err = os.WriteFile(fullPath, []byte(content), 0o600)
	assert.NoError(t, err)
}
