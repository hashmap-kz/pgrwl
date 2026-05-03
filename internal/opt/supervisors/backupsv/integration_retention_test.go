//go:build integration_localdev

package backupsv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/opt/basebackup/backupdto"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

// TestIntegrationRetentionLocaldev exercises the real recovery-window
// retention path against a local SeaweedFS S3 gateway, used through the real
// S3 storage backend.
//
// It intentionally writes objects using the real pgrwl layout:
//
//	<run-id>/backups/<backup-id>/<backup-id>.json
//	<run-id>/backups/<backup-id>/base.tar
//	<run-id>/backups/<backup-id>/25222.tar
//	<run-id>/wal-archive/<wal-file>
func TestIntegrationRetentionLocaldev(t *testing.T) {
	env := loadRetentionIntegrationEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	walSegSz := uint64(16 * 1024 * 1024)

	s3Client := newIntegrationS3Client(t, ctx, env)
	ensureIntegrationBucket(t, ctx, s3Client, env.bucket)

	runPrefix := fmt.Sprintf("pgrwl-retention-it/%d", time.Now().UTC().UnixNano())
	backupStorage := st.NewS3Storage(s3Client, env.bucket, runPrefix+"/backups")
	walRawStorage := st.NewS3Storage(s3Client, env.bucket, runPrefix+"/wal-archive")
	walStorage, err := st.NewVariadicStorage(walRawStorage, st.Algorithms{}, "")
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()

		// Delete through both scoped storages first, then delete the whole run prefix
		// as a best-effort safety net.
		_ = backupStorage.DeleteAll(cleanupCtx, "")
		_ = walRawStorage.DeleteAll(cleanupCtx, "")
		_ = st.NewS3Storage(s3Client, env.bucket, runPrefix).DeleteAll(cleanupCtx, "")
	})

	now := time.Now().UTC()

	// With a 1h recovery window, the newest backup before windowStart is the
	// anchor. Here that should be 20260502070500. The older 20260502065500
	// backup should be removed, while anchor/newer backups remain.
	putIntegrationBackupManifest(t, ctx, backupStorage, "20260502065500", now.Add(-4*time.Hour), 1, "3C/D7000000")
	putIntegrationBackupManifest(t, ctx, backupStorage, "20260502070500", now.Add(-2*time.Hour), 1, "3C/D9000000")
	putIntegrationBackupManifest(t, ctx, backupStorage, "20260502071500", now.Add(-30*time.Minute), 1, "3C/DA000000")

	// Broken manifests should be skipped and, importantly, not deleted by this
	// retention run because they are not considered successful backups.
	putIntegrationObject(t, ctx, backupStorage, "20260502070000/20260502070000.json", "{broken-json")
	putIntegrationObject(t, ctx, backupStorage, "20260502070000/base.tar", "broken backup payload")

	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000D7", "wal")
	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000D8", "wal")
	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000D9", "wal")
	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000DA", "wal")
	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000DB", "wal")
	putIntegrationObject(t, ctx, walRawStorage, "000000010000003C000000DC.partial", "partial")
	putIntegrationObject(t, ctx, walRawStorage, "00000002.history", "history")
	putIntegrationObject(t, ctx, walRawStorage, "README.txt", "not a wal")

	keepLast := 1
	cfg := &config.Config{
		Retention: config.RetentionConfig{
			Enable:             true,
			Type:               config.RetentionTypeRecoveryWindow,
			KeepDurationParsed: time.Hour,
			KeepLast:           &keepLast,
		},
	}

	retention := NewRecoveryWindowRetention(
		&BackupSupervisorOpts{
			WalSegSz:       walSegSz,
			BasebackupStor: backupStorage,
			WalStor:        walStorage,
			Cfg:            cfg,
		},
	)

	err = retention.RunBeforeBackup(ctx)
	require.NoError(t, err)

	assertIntegrationMissing(t, ctx, backupStorage, "20260502065500/20260502065500.json")
	assertIntegrationMissing(t, ctx, backupStorage, "20260502065500/base.tar")
	assertIntegrationMissing(t, ctx, backupStorage, "20260502065500/25222.tar")

	assertIntegrationExists(t, ctx, backupStorage, "20260502070500/20260502070500.json")
	assertIntegrationExists(t, ctx, backupStorage, "20260502071500/20260502071500.json")

	// Broken/unreadable backup manifest is skipped, not deleted.
	assertIntegrationExists(t, ctx, backupStorage, "20260502070000/20260502070000.json")
	assertIntegrationExists(t, ctx, backupStorage, "20260502070000/base.tar")

	assertIntegrationMissing(t, ctx, walRawStorage, "000000010000003C000000D7")
	assertIntegrationMissing(t, ctx, walRawStorage, "000000010000003C000000D8")
	assertIntegrationExists(t, ctx, walRawStorage, "000000010000003C000000D9")
	assertIntegrationExists(t, ctx, walRawStorage, "000000010000003C000000DA")
	assertIntegrationExists(t, ctx, walRawStorage, "000000010000003C000000DB")
	assertIntegrationExists(t, ctx, walRawStorage, "000000010000003C000000DC.partial")
	assertIntegrationExists(t, ctx, walRawStorage, "00000002.history")
	assertIntegrationExists(t, ctx, walRawStorage, "README.txt")
}

type retentionIntegrationEnv struct {
	s3Endpoint       string
	s3AccessKey      string
	s3SecretKey      string
	bucket           string
	region           string
	usePathStyle     bool
	disableTLSVerify bool
}

func loadRetentionIntegrationEnv(t *testing.T) retentionIntegrationEnv {
	t.Helper()

	s3Endpoint := "http://127.0.0.1:8333"

	return retentionIntegrationEnv{
		s3Endpoint:       s3Endpoint,
		s3AccessKey:      "pgrwl",
		s3SecretKey:      "pgrwl-secret",
		bucket:           "backups",
		region:           "us-east-1",
		usePathStyle:     true,
		disableTLSVerify: true,
	}
}

func newIntegrationS3Client(t *testing.T, ctx context.Context, env retentionIntegrationEnv) *s3.Client {
	t.Helper()

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(env.region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(env.s3AccessKey, env.s3SecretKey, "")),
	)
	require.NoError(t, err)

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(env.s3Endpoint)
		o.UsePathStyle = env.usePathStyle
	})
}

func ensureIntegrationBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return
	}

	var bucketAlreadyOwned *s3types.BucketAlreadyOwnedByYou
	var bucketAlreadyExists *s3types.BucketAlreadyExists
	if errors.As(err, &bucketAlreadyOwned) || errors.As(err, &bucketAlreadyExists) {
		return
	}

	// Some S3-compatible stores return generic API errors for existing buckets.
	if strings.Contains(strings.ToLower(err.Error()), "bucketalready") || strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return
	}

	require.NoError(t, err)
}

func putIntegrationBackupManifest(
	t *testing.T,
	ctx context.Context,
	storage st.Storage,
	backupID string,
	startedAt time.Time,
	timeline int32,
	startLSN string,
) {
	t.Helper()

	lsn, err := pglogrepl.ParseLSN(startLSN)
	require.NoError(t, err)

	result := backupdto.Result{
		StartLSN:   lsn,
		TimelineID: timeline,
		StartedAt:  startedAt.UTC(),
		FinishedAt: startedAt.Add(5 * time.Minute).UTC(),
		Manifest: &backupdto.BackupManifest{
			Version: 1,
			WALRanges: []backupdto.ManifestWALRange{
				{
					Timeline: timeline,
					StartLSN: startLSN,
					EndLSN:   startLSN,
				},
			},
		},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)

	putIntegrationObject(t, ctx, storage, backupID+"/"+backupID+".json", string(data))
	putIntegrationObject(t, ctx, storage, backupID+"/base.tar", "base payload")
	putIntegrationObject(t, ctx, storage, backupID+"/25222.tar", "tablespace payload")
}

func putIntegrationObject(t *testing.T, ctx context.Context, storage st.Storage, path string, body string) {
	t.Helper()

	require.NoError(t, storage.Put(ctx, path, strings.NewReader(body)))
}

func assertIntegrationExists(t *testing.T, ctx context.Context, storage st.Storage, path string) {
	t.Helper()

	exists, err := storage.Exists(ctx, path)
	require.NoError(t, err)
	assert.True(t, exists, "expected %s to exist", path)
}

func assertIntegrationMissing(t *testing.T, ctx context.Context, storage st.Storage, path string) {
	t.Helper()

	exists, err := storage.Exists(ctx, path)
	if err != nil && isIntegrationNotFoundErr(err) {
		exists = false
		err = nil
	}
	require.NoError(t, err)
	assert.False(t, exists, "expected %s to be deleted", path)
}

func isIntegrationNotFoundErr(err error) bool {
	if err == nil {
		return false
	}

	var responseErr interface{ HTTPStatusCode() int }
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}

	return strings.Contains(strings.ToLower(err.Error()), "notfound") || strings.Contains(strings.ToLower(err.Error()), "not found")
}
