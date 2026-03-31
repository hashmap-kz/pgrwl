package codec

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockCompressor implements Compressor for testing
type MockCompressor struct {
	ext  string
	name string
}

func (m *MockCompressor) NewWriter(_ io.Writer) (WriteFlushCloser, error) {
	return nil, nil
}

func (m *MockCompressor) FileExtension() string {
	return m.ext
}

func (m *MockCompressor) Name() string {
	return m.name
}

func TestGetDecompressor_Gzip(t *testing.T) {
	c := &MockCompressor{ext: GzipFileExt}
	d := GetDecompressor(c)
	assert.NotNil(t, d)
	assert.IsType(t, &GzipDecompressor{}, d)
}

func TestGetDecompressor_Zstd(t *testing.T) {
	c := &MockCompressor{ext: ZstdFileExt}
	d := GetDecompressor(c)
	assert.NotNil(t, d)
	assert.IsType(t, &ZstdDecompressor{}, d)
}

func TestGetDecompressor_Unknown(t *testing.T) {
	c := &MockCompressor{ext: ".unknown"}
	d := GetDecompressor(c)
	assert.Nil(t, d)
}

func TestGetDecompressor_Nil(t *testing.T) {
	d := GetDecompressor(nil)
	assert.Nil(t, d)
}
