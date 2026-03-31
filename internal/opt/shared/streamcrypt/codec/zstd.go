package codec

import (
	"io"

	"github.com/klauspost/compress/zstd"
)

// Zstd Compressor

type ZstdCompressor struct{}

var _ Compressor = &ZstdCompressor{}

func (ZstdCompressor) FileExtension() string {
	return ZstdFileExt
}

func (ZstdCompressor) NewWriter(w io.Writer) (WriteFlushCloser, error) {
	zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	return zw, nil
}

func (ZstdCompressor) Name() string {
	return ZstdCompName
}

// Zstd Decompressor

type ZstdDecompressor struct{}

var _ Decompressor = &ZstdDecompressor{}

func (ZstdDecompressor) FileExtension() string {
	return ZstdFileExt
}

func (ZstdDecompressor) Decompress(r io.Reader) (io.ReadCloser, error) {
	decoder, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(decoder), nil
}
