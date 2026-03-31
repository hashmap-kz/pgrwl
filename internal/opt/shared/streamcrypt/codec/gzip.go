package codec

import (
	"compress/gzip"
	"io"
)

// Gzip Compressor

type GzipCompressor struct{}

var _ Compressor = &GzipCompressor{}

func (GzipCompressor) FileExtension() string {
	return GzipFileExt
}

func (GzipCompressor) NewWriter(w io.Writer) (WriteFlushCloser, error) {
	gw := gzip.NewWriter(w)
	return &gzipWrapper{Writer: gw}, nil
}

func (GzipCompressor) Name() string {
	return GzipCompName
}

type gzipWrapper struct {
	*gzip.Writer
}

func (g *gzipWrapper) Flush() error {
	return g.Writer.Flush()
}

func (g *gzipWrapper) Close() error {
	return g.Writer.Close()
}

// Gzip Decompressor

type GzipDecompressor struct{}

var _ Decompressor = &GzipDecompressor{}

func (GzipDecompressor) FileExtension() string {
	return GzipFileExt
}

func (GzipDecompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}
