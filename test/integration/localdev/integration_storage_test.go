//go:build integration_localdev

package localdev

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

// TestIntegrationLocaldev_ListDoesNotReturnPrefixSiblings verifies that
// List("base") returns only the contents of "base/", not siblings like "base-old/".
func TestIntegrationLocaldev_ListDoesNotReturnPrefixSiblings(t *testing.T) {
	env := loadRetentionIntegrationEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s3Client := newIntegrationS3Client(t, ctx, env)
	ensureIntegrationBucket(t, ctx, s3Client, env.bucket)

	runPrefix := fmt.Sprintf("pgrwl-storage-it/%d", time.Now().UTC().UnixNano())
	storage := st.NewS3Storage(s3Client, env.bucket, runPrefix)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = storage.DeleteAll(cleanupCtx, "")
	})

	putIntegrationObject(t, ctx, storage, "20260502070500/manifest.json", `{}`)
	putIntegrationObject(t, ctx, storage, "20260502070500-old/manifest.json", `{}`)
	putIntegrationObject(t, ctx, storage, "20260502070500X/manifest.json", `{}`)

	listed, err := storage.List(ctx, "20260502070500")
	require.NoError(t, err)

	assert.Contains(t, listed, "20260502070500/manifest.json", "target backup should be listed")
	for _, key := range listed {
		assert.True(t, strings.HasPrefix(key, "20260502070500/"),
			"List returned sibling key %q - expected only \"20260502070500/\" prefix", key)
	}
}

// TestIntegrationLocaldev_DeleteDirDoesNotDeleteSiblings verifies that
// DeleteDir("backupA") leaves sibling prefixes like "backupA-old/" intact.
func TestIntegrationLocaldev_DeleteDirDoesNotDeleteSiblings(t *testing.T) {
	env := loadRetentionIntegrationEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s3Client := newIntegrationS3Client(t, ctx, env)
	ensureIntegrationBucket(t, ctx, s3Client, env.bucket)

	runPrefix := fmt.Sprintf("pgrwl-storage-it/%d", time.Now().UTC().UnixNano())
	storage := st.NewS3Storage(s3Client, env.bucket, runPrefix)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = storage.DeleteAll(cleanupCtx, "")
	})

	putIntegrationObject(t, ctx, storage, "20260502070500/20260502070500.json", `{}`)
	putIntegrationObject(t, ctx, storage, "20260502070500/base.tar", "base payload")
	putIntegrationObject(t, ctx, storage, "20260502070500-old/20260502070500-old.json", `{}`)
	putIntegrationObject(t, ctx, storage, "20260502070500X/20260502070500X.json", `{}`)

	err := storage.DeleteDir(ctx, "20260502070500")
	require.NoError(t, err)

	assertIntegrationMissing(t, ctx, storage, "20260502070500/20260502070500.json")
	assertIntegrationMissing(t, ctx, storage, "20260502070500/base.tar")

	assertIntegrationExists(t, ctx, storage, "20260502070500-old/20260502070500-old.json")
	assertIntegrationExists(t, ctx, storage, "20260502070500X/20260502070500X.json")
}

// TestIntegrationLocaldev_EmptyObjectRoundTrip verifies that a zero-byte object
// can be stored and retrieved correctly (regression for the multipart abort path).
func TestIntegrationLocaldev_EmptyObjectRoundTrip(t *testing.T) {
	env := loadRetentionIntegrationEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	s3Client := newIntegrationS3Client(t, ctx, env)
	ensureIntegrationBucket(t, ctx, s3Client, env.bucket)

	runPrefix := fmt.Sprintf("pgrwl-storage-it/%d", time.Now().UTC().UnixNano())
	storage := st.NewS3Storage(s3Client, env.bucket, runPrefix)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = storage.DeleteAll(cleanupCtx, "")
	})

	err := storage.Put(ctx, "empty.json", strings.NewReader(""))
	require.NoError(t, err)

	exists, err := storage.Exists(ctx, "empty.json")
	require.NoError(t, err)
	assert.True(t, exists, "zero-byte object should exist after Put")

	rc, err := storage.Get(ctx, "empty.json")
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Empty(t, data, "zero-byte object body should be empty on Get")
}
