//nolint:dupl
package codec

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZstdCompressor_RoundTrip(t *testing.T) {
	original := []byte("this is a test of zstd compression")

	var buf bytes.Buffer
	comp := ZstdCompressor{}

	writer, err := comp.NewWriter(&buf)
	require.NoError(t, err)

	_, err = writer.Write(original)
	require.NoError(t, err)
	assert.NoError(t, writer.Flush())
	assert.NoError(t, writer.Close())

	// Decompress
	decomp := ZstdDecompressor{}
	reader, err := decomp.Decompress(&buf)
	require.NoError(t, err)
	defer reader.Close()

	result, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

func TestZstdCompressor_Metadata(t *testing.T) {
	comp := ZstdCompressor{}
	assert.Equal(t, ".zst", comp.FileExtension())
	assert.Equal(t, "zstd", comp.Name())

	decomp := ZstdDecompressor{}
	assert.Equal(t, ".zst", decomp.FileExtension())
}
