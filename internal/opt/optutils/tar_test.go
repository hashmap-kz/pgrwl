package optutils

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

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
