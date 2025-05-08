package xlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a regular file
	regularFile := filepath.Join(tmpDir, "testfile.txt")
	err := os.WriteFile(regularFile, []byte("hello"), 0o600)
	assert.NoError(t, err)

	// Create a directory
	subDir := filepath.Join(tmpDir, "subdir")
	err = os.Mkdir(subDir, 0o755)
	assert.NoError(t, err)

	// Non-existent path
	nonExistent := filepath.Join(tmpDir, "does_not_exist")

	t.Run("regular file exists", func(t *testing.T) {
		assert.True(t, fileExists(regularFile))
	})

	t.Run("directory does not count as file", func(t *testing.T) {
		assert.False(t, fileExists(subDir))
	})

	t.Run("non-existent file", func(t *testing.T) {
		assert.False(t, fileExists(nonExistent))
	})
}
