package storecrypt

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeS3PartSize(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want int64
	}{
		{
			name: "zero uses default",
			in:   0,
			want: DefaultS3PartSize,
		},
		{
			name: "negative uses default",
			in:   -1,
			want: DefaultS3PartSize,
		},
		{
			name: "below minimum is clamped to minimum",
			in:   1,
			want: MinS3PartSize,
		},
		{
			name: "minimum is kept",
			in:   MinS3PartSize,
			want: MinS3PartSize,
		},
		{
			name: "normal value is kept",
			in:   64 * 1024 * 1024,
			want: 64 * 1024 * 1024,
		},
		{
			name: "above maximum is clamped to maximum",
			in:   MaxS3PartSize + 1,
			want: MaxS3PartSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeS3PartSize(tt.in))
		})
	}
}

func TestNormalizeConcurrency(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{
			name: "zero uses default",
			in:   0,
			want: DefaultS3Conc,
		},
		{
			name: "negative uses default",
			in:   -1,
			want: DefaultS3Conc,
		},
		{
			name: "normal value is kept",
			in:   8,
			want: 8,
		},
		{
			name: "above maximum is clamped",
			in:   MaxS3Conc + 1,
			want: MaxS3Conc,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeConcurrency(tt.in))
		})
	}
}

func TestCleanS3Prefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{
			name:   "empty",
			prefix: "",
			want:   "",
		},
		{
			name:   "slash only",
			prefix: "/",
			want:   "",
		},
		{
			name:   "dot",
			prefix: ".",
			want:   "",
		},
		{
			name:   "trims leading slash",
			prefix: "/backups",
			want:   "backups",
		},
		{
			name:   "trims trailing slash",
			prefix: "backups/",
			want:   "backups",
		},
		{
			name:   "trims both sides",
			prefix: "/backups/base/",
			want:   "backups/base",
		},
		{
			name:   "cleans duplicate slashes",
			prefix: "backups//base",
			want:   "backups/base",
		},
		{
			name:   "cleans dot segment",
			prefix: "backups/./base",
			want:   "backups/base",
		},
		{
			name:   "cleans parent segment",
			prefix: "backups/tmp/../base",
			want:   "backups/base",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cleanS3Prefix(tt.prefix))
		})
	}
}

func TestJoinS3Key(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{
			name:   "empty prefix empty key",
			prefix: "",
			key:    "",
			want:   "",
		},
		{
			name:   "empty prefix dot key",
			prefix: "",
			key:    ".",
			want:   "",
		},
		{
			name:   "empty prefix normal key",
			prefix: "",
			key:    "file.txt",
			want:   "file.txt",
		},
		{
			name:   "empty prefix leading slash key",
			prefix: "",
			key:    "/file.txt",
			want:   "file.txt",
		},
		{
			name:   "prefix empty key",
			prefix: "backups",
			key:    "",
			want:   "backups",
		},
		{
			name:   "prefix dot key",
			prefix: "backups",
			key:    ".",
			want:   "backups",
		},
		{
			name:   "prefix normal key",
			prefix: "backups",
			key:    "base/manifest.json",
			want:   "backups/base/manifest.json",
		},
		{
			name:   "prefix leading slash key",
			prefix: "backups",
			key:    "/base/manifest.json",
			want:   "backups/base/manifest.json",
		},
		{
			name:   "cleans duplicate slash in key",
			prefix: "backups",
			key:    "base//manifest.json",
			want:   "backups/base/manifest.json",
		},
		{
			name:   "cleans dot segment in key",
			prefix: "backups",
			key:    "base/./manifest.json",
			want:   "backups/base/manifest.json",
		},
		{
			name:   "cleans parent segment in key",
			prefix: "backups",
			key:    "base/tmp/../manifest.json",
			want:   "backups/base/manifest.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, joinS3Key(tt.prefix, tt.key))
		})
	}
}

func TestS3StorageFullPath(t *testing.T) {
	s := &s3Storage{prefix: cleanS3Prefix("/repo/backups/")}

	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "empty key returns prefix",
			key:  "",
			want: "repo/backups",
		},
		{
			name: "normal key",
			key:  "base/manifest.json",
			want: "repo/backups/base/manifest.json",
		},
		{
			name: "leading slash key",
			key:  "/base/manifest.json",
			want: "repo/backups/base/manifest.json",
		},
		{
			name: "cleans key",
			key:  "base//manifest.json",
			want: "repo/backups/base/manifest.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, s.fullPath(tt.key))
		})
	}
}

func TestS3StorageRelativeKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{
			name:   "empty prefix returns key",
			prefix: "",
			key:    "repo/backups/file.txt",
			want:   "repo/backups/file.txt",
		},
		{
			name:   "empty prefix trims leading slash",
			prefix: "",
			key:    "/repo/backups/file.txt",
			want:   "repo/backups/file.txt",
		},
		{
			name:   "key equal prefix returns empty",
			prefix: "repo/backups",
			key:    "repo/backups",
			want:   "",
		},
		{
			name:   "key below prefix returns relative path",
			prefix: "repo/backups",
			key:    "repo/backups/base/manifest.json",
			want:   "base/manifest.json",
		},
		{
			name:   "leading slash key below prefix returns relative path",
			prefix: "repo/backups",
			key:    "/repo/backups/base/manifest.json",
			want:   "base/manifest.json",
		},
		{
			name:   "partial prefix match is not stripped",
			prefix: "repo/backups",
			key:    "repo/backups-old/base/manifest.json",
			want:   "repo/backups-old/base/manifest.json",
		},
		{
			name:   "unrelated key is returned unchanged",
			prefix: "repo/backups",
			key:    "other/file.txt",
			want:   "other/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &s3Storage{prefix: cleanS3Prefix(tt.prefix)}
			assert.Equal(t, tt.want, s.relativeKey(tt.key))
		})
	}
}

func TestChooseUploadPartSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want int64
	}{
		{
			name: "zero uses default known-size part size",
			size: 0,
			want: DefaultS3PartSize,
		},
		{
			name: "negative uses default known-size part size",
			size: -1,
			want: DefaultS3PartSize,
		},
		{
			name: "small object uses min part size",
			size: 16 * 1024 * 1024,
			want: MinS3PartSize,
		},
		{
			name: "50GiB rounds up to 6MiB",
			size: 50 * 1024 * 1024 * 1024,
			want: 6 * 1024 * 1024,
		},
		{
			name: "500GiB rounds up to 52MiB",
			size: 500 * 1024 * 1024 * 1024,
			want: 52 * 1024 * 1024,
		},
		{
			name: "huge object clamps to max part size",
			size: MaxS3PartSize*MaxS3UploadParts + 1,
			want: MaxS3PartSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, chooseUploadPartSize(tt.size))
		})
	}
}

func TestEndsWithSlash(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "empty",
			in:   "",
			want: false,
		},
		{
			name: "no slash",
			in:   "backups",
			want: false,
		},
		{
			name: "with slash",
			in:   "backups/",
			want: true,
		},
		{
			name: "slash only",
			in:   "/",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, endsWithSlash(tt.in))
		})
	}
}

func TestIsSeekable(t *testing.T) {
	t.Run("os file is seekable", func(t *testing.T) {
		f, err := os.CreateTemp(t.TempDir(), "seekable-*")
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, f.Close())
		}()

		got, ok := isSeekable(f)

		assert.True(t, ok)
		assert.Same(t, f, got)
	})

	t.Run("bytes reader is not treated as seekable by current helper", func(t *testing.T) {
		got, ok := isSeekable(strings.NewReader("hello"))

		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func TestChooseUploadPartSize_StaysWithinS3PartLimit(t *testing.T) {
	sizes := []int64{
		1 << 30,   // 1 GiB
		50 << 30,  // 50 GiB
		500 << 30, // 500 GiB
		1 << 40,   // 1 TiB
		2 << 40,   // 2 TiB
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			partSize := chooseUploadPartSize(size)

			assert.GreaterOrEqual(t, partSize, MinS3PartSize)
			assert.LessOrEqual(t, partSize, MaxS3PartSize)

			parts := (size + partSize - 1) / partSize
			assert.LessOrEqual(t, parts, MaxS3UploadParts)
		})
	}
}
