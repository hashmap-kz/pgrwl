//go:build integration_storage

package integration

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/pgrwl/pgrwl/internal/opt/shared/fakereaders"
	storage "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/require"
)

func TestS3Storage_PutMultipart50Gi(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	client := createS3Client()
	prefix := t.Name()
	st := storage.NewS3Storage(client, "backups", prefix)

	r := fakereaders.NewFakeLargeReader(fakereaders.Size50Gi)
	// wrap to hide size (IMPORTANT)
	reader := io.NopCloser(r)

	err := st.Put(ctx, "stream.bin", reader)
	require.NoError(t, err)

	info, err := st.ListInfo(ctx, "")
	require.NoError(t, err)

	require.Equal(t, 1, len(info))
	require.Equal(t, fakereaders.Size50Gi, info[0].Size)
}
