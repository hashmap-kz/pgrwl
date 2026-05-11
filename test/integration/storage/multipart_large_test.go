//go:build integration_storage

package integration

// These tests verify S3 multipart-upload protocol correctness at 1 TiB scale.
// A custom http.RoundTripper is injected into the minio client so all requests
// are intercepted in-process: no real MinIO, no TCP, no disk data written
// (the seekable-file test uses a sparse file - Stat().Size() == 1 TiB, actual
// disk use is one filesystem block).

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	storage "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multipartSpy records S3 multipart-protocol calls. Request bodies are
// drained and discarded so no data accumulates beyond what minio already holds.
type multipartSpy struct {
	mu            sync.Mutex
	createCount   int
	partCount     int
	completeCount int
}

type fakeS3Transport struct{ spy *multipartSpy }

func (tr *fakeS3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	q := req.URL.Query()

	drain := func() {
		if req.Body != nil {
			io.Copy(io.Discard, req.Body) //nolint:errcheck
			req.Body.Close()
		}
	}

	switch {
	case req.Method == http.MethodGet && q.Has("location"):
		drain()
		return fakeXMLResp(200, `<?xml version="1.0" encoding="UTF-8"?>
<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></LocationConstraint>`), nil

	case req.Method == http.MethodPost && q.Has("uploads"):
		drain()
		tr.spy.mu.Lock()
		tr.spy.createCount++
		tr.spy.mu.Unlock()
		return fakeXMLResp(200, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>backups</Bucket><Key>%s</Key><UploadId>spy-upload-id</UploadId>
</InitiateMultipartUploadResult>`, req.URL.Path)), nil

	case req.Method == http.MethodPut && q.Has("partNumber"):
		// Body is an in-memory bytes.Reader - draining is effectively instant.
		drain()
		tr.spy.mu.Lock()
		tr.spy.partCount++
		tr.spy.mu.Unlock()
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Etag": []string{`"part-etag"`}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil

	case req.Method == http.MethodPost && q.Has("uploadId"):
		drain()
		tr.spy.mu.Lock()
		tr.spy.completeCount++
		tr.spy.mu.Unlock()
		return fakeXMLResp(200, fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Location>http://fake-s3%s</Location><Bucket>backups</Bucket>
  <Key>%s</Key><ETag>"etag-spy-done"</ETag>
</CompleteMultipartUploadResult>`, req.URL.Path, req.URL.Path)), nil

	case req.Method == http.MethodDelete && q.Has("uploadId"):
		drain()
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader(""))}, nil

	case req.Method == http.MethodHead:
		drain()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil

	default:
		return nil, fmt.Errorf("unexpected S3 request: %s %s", req.Method, req.URL)
	}
}

func fakeXMLResp(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newSpyMinioClient(t *testing.T) (*minio.Client, *multipartSpy) {
	t.Helper()
	spy := &multipartSpy{}
	client, err := minio.New("fake-s3.local:80", &minio.Options{
		Creds:        credentials.NewStaticV4("key", "secret", ""),
		Secure:       false,
		BucketLookup: minio.BucketLookupPath,
		Transport:    &fakeS3Transport{spy: spy},
	})
	require.NoError(t, err)
	return client, spy
}

// TestS3Storage_Put_50GiB_Stream_MultipartProtocol verifies that uploading a
// 50 GiB unknown-size stream (size=-1) results in exactly
// 50 GiB / MultipartDefaultPartSizeBytes (200) UploadPart calls and that
// CreateMultipartUpload / CompleteMultipartUpload are each called once.
func TestS3Storage_Put_50GiB_Stream_MultipartProtocol(t *testing.T) {
	t.Parallel()

	client, spy := newSpyMinioClient(t)
	st := storage.NewS3Storage(client, "backups", "test")

	const size int64 = 50 << 30 // 50 GiB
	src := &patternReader{remaining: size}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := st.Put(ctx, "1tib-stream", src)
	require.NoError(t, err)

	const expectedParts = size / storage.MultipartDefaultPartSizeBytes // 4096

	assert.Equal(t, 1, spy.createCount, "CreateMultipartUpload must be called exactly once")
	assert.Equal(t, int(expectedParts), spy.partCount, "UploadPart call count")
	assert.Equal(t, 1, spy.completeCount, "CompleteMultipartUpload must be called exactly once")
}

// TestS3Storage_Put_50GiB_SeekableFile_PartScaling verifies that uploading a
// 50 GiB seekable *os.File triggers minio's part-size auto-scaling: at 5 MiB
// base size this would produce 10 240 parts (> 10 000 S3 limit), so minio must
// scale up. A sparse file is used so only a single filesystem block is
// allocated on disk.
func TestS3Storage_Put_50GiB_SeekableFile_PartScaling(t *testing.T) {
	t.Parallel()

	f := createSparseFile(t, 50<<30) // 50 GiB sparse
	defer f.Close()

	client, spy := newSpyMinioClient(t)
	st := storage.NewS3Storage(client, "backups", "test")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	err := st.Put(ctx, "1tib-seekable", f)
	require.NoError(t, err)

	assert.Equal(t, 1, spy.createCount, "CreateMultipartUpload must be called exactly once")
	assert.Greater(t, spy.partCount, 0, "at least one UploadPart must be sent")
	// minio auto-scales from MinS3PartSize so parts stay within the S3 limit.
	assert.LessOrEqual(t, spy.partCount, 10000, "part count must not exceed S3 limit")
	assert.Equal(t, 1, spy.completeCount, "CompleteMultipartUpload must be called exactly once")
}
