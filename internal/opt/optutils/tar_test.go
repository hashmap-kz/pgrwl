package optutils

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func createTestTar(t *testing.T, files map[string]string) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for name, content := range files {
		data := []byte(content)
		err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o600,
			Size: int64(len(data)),
		})
		assert.NoError(t, err)

		_, err = tw.Write(data)
		assert.NoError(t, err)
	}
	assert.NoError(t, tw.Close())
	return &buf
}

func TestGetFileFromTar_Found(t *testing.T) {
	files := map[string]string{
		"foo.txt": "hello world",
		"bar.txt": "another file",
	}
	tarReader := createTestTar(t, files)

	rc, err := GetFileFromTar(tarReader, "foo.txt")
	assert.NoError(t, err)
	assert.NotNil(t, rc)
	defer rc.Close()

	content, err := io.ReadAll(rc)
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestGetFileFromTar_NotFound(t *testing.T) {
	files := map[string]string{
		"foo.txt": "hello world",
	}
	tarReader := createTestTar(t, files)

	rc, err := GetFileFromTar(tarReader, "missing.txt")
	assert.Nil(t, rc)
	assert.Error(t, err)
	assert.Equal(t, "file not found in tar", err.Error())
}

func TestCreateTarReader(t *testing.T) {
	// Setup: create temp files
	dir := t.TempDir()
	files := []string{}
	contentMap := map[string]string{
		"file1.txt": "hello",
		"file2.txt": "world",
	}

	for name, content := range contentMap {
		path := filepath.Join(dir, name)
		err := os.WriteFile(path, []byte(content), 0o644)
		require.NoError(t, err)
		files = append(files, path)
	}

	// Call the function
	rc := CreateTarReader(files)
	defer rc.Close()

	// Read the tar stream
	tr := tar.NewReader(rc)
	found := map[string]string{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		var buf bytes.Buffer
		_, err = io.Copy(&buf, tr)
		require.NoError(t, err)

		found[hdr.Name] = buf.String()
	}

	// Assert all files were found and contents match
	require.Equal(t, len(contentMap), len(found))
	for name, expected := range contentMap {
		require.Equal(t, expected, found[name])
	}
}
