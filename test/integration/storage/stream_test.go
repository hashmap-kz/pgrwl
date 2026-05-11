//go:build integration_storage

package integration

import (
	"context"
	"io"
	"testing"

	"github.com/minio/minio-go/v7"
	storage "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/require"
)

type generatedReader struct {
	remaining int64
}

func (r *generatedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}

	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}

	// Zero bytes are fine. We test transport/storage size, not entropy.
	for i := range p {
		p[i] = byte(i)
	}

	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestS3StoragePutUnknownSizeStreamMultipart(t *testing.T) {
	ctx := context.Background()

	bucket := getenv("S3_BUCKET", "pgrwl-test")
	client := createS3Client()

	exists, err := client.BucketExists(ctx, bucket)
	require.NoError(t, err)

	if !exists {
		require.NoError(t, client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}))
	}

	st := storage.NewS3StorageWithOptions(client, bucket, "integration/storage", storage.S3Options{
		// Small enough for CI, still forces multipart for unknown stream.
		PartSizeBytes: storage.MinS3PartSize,
		Concurrency:   2,
	})

	objectSize := getenvInt64(t, "S3_LARGE_STREAM_SIZE", 32*1024*1024)

	err = st.Put(ctx, "unknown-stream.bin", &generatedReader{remaining: objectSize})
	require.NoError(t, err)

	info, err := client.StatObject(ctx, bucket, "integration/storage/unknown-stream.bin", minio.StatObjectOptions{})
	require.NoError(t, err)
	require.Equal(t, objectSize, info.Size)

	require.NoError(t, st.Delete(ctx, "unknown-stream.bin"))
}
