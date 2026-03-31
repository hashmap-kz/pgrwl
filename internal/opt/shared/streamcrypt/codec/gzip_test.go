//nolint:dupl
package codec

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGzipCompressor_RoundTrip(t *testing.T) {
	original := []byte("this is a test of gzip compression")

	var buf bytes.Buffer
	comp := GzipCompressor{}

	writer, err := comp.NewWriter(&buf)
	require.NoError(t, err)

	_, err = writer.Write(original)
	require.NoError(t, err)
	assert.NoError(t, writer.Flush())
	assert.NoError(t, writer.Close())

	// Decompress
	decomp := GzipDecompressor{}
	reader, err := decomp.Decompress(&buf)
	require.NoError(t, err)
	defer reader.Close()

	result, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

func TestGzipCompressor_Metadata(t *testing.T) {
	comp := GzipCompressor{}
	assert.Equal(t, ".gz", comp.FileExtension())
	assert.Equal(t, "gzip", comp.Name())

	decomp := GzipDecompressor{}
	assert.Equal(t, ".gz", decomp.FileExtension())
}
