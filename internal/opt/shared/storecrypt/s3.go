package storecrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	MinS3PartSize     int64 = 5 * 1024 * 1024
	DefaultS3PartSize int64 = 16 * 1024 * 1024
	DefaultS3Conc           = 2
	MaxS3UploadParts  int64 = 10000
)

type S3Options struct {
	PartSizeBytes int64
	Concurrency   int
}

type s3Storage struct {
	client   *s3.Client
	bucket   string
	prefix   string
	uploader *transfermanager.Client
}

var _ Storage = &s3Storage{}

func NewS3Storage(client *s3.Client, bucket, prefix string) Storage {
	return NewS3StorageWithOptions(client, bucket, prefix, S3Options{})
}

func NewS3StorageWithOptions(client *s3.Client, bucket, prefix string, opts S3Options) Storage {
	partSize := opts.PartSizeBytes
	if partSize <= 0 {
		partSize = DefaultS3PartSize
	}
	if partSize < MinS3PartSize {
		partSize = MinS3PartSize
	}

	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultS3Conc
	}

	tmClient := transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = partSize
		o.Concurrency = concurrency
	})

	return &s3Storage{
		client:   client,
		bucket:   bucket,
		prefix:   filepath.ToSlash(strings.TrimPrefix(prefix, "/")),
		uploader: tmClient,
	}
}

func (s *s3Storage) fullPath(path string) string {
	return filepath.ToSlash(filepath.Join(s.prefix, path))
}

// CreateUploader creates a new S3 uploader with the given part size and concurrency.
func CreateUploader(client *s3.Client, partSize int64, concurrency int) *transfermanager.Client {
	if partSize <= 0 {
		partSize = DefaultS3PartSize
	}
	if partSize < MinS3PartSize {
		partSize = MinS3PartSize
	}
	if concurrency <= 0 {
		concurrency = DefaultS3Conc
	}

	return transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = partSize
		o.Concurrency = concurrency
	})
}

// ChooseUploadPartSize returns a safe part size for an expected object size.
// If size <= 0, it returns a good streaming default.
func ChooseUploadPartSize(size int64) int64 {
	if size <= 0 {
		return DefaultS3PartSize
	}

	partSize := (size + MaxS3UploadParts - 1) / MaxS3UploadParts
	if partSize < MinS3PartSize {
		partSize = MinS3PartSize
	}

	// round up to whole MiB for cleaner values
	const mib = int64(1024 * 1024)
	if rem := partSize % mib; rem != 0 {
		partSize += mib - rem
	}

	return partSize
}

func (s *s3Storage) Put(ctx context.Context, remotePath string, r io.Reader) error {
	remotePath = s.fullPath(remotePath)

	// If we know the size, use transfermanager with computed part size.
	if f, ok := isSeekable(r); ok {
		st, err := f.Stat()
		if err == nil {
			size := st.Size()
			partSize := ChooseUploadPartSize(size)
			uploader := CreateUploader(s.client, partSize, DefaultS3Conc)

			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("seek file for %q: %w", remotePath, err)
			}

			_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    aws.String(remotePath),
				Body:   f,
			})
			if err != nil {
				return fmt.Errorf("s3 upload %q: %w", remotePath, err)
			}
			return nil
		}
	}

	// Unknown-size stream: use manual multipart with a conservative part size.
	// 256 MiB gives ~2.44 TiB before hitting 10k parts.
	return s.putMultipartStream(ctx, remotePath, r, 256*1024*1024)
}

func (s *s3Storage) Get(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	remotePath = s.fullPath(remotePath)

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remotePath),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read object from S3: %w", err)
	}
	return out.Body, nil
}

func (s *s3Storage) List(ctx context.Context, remotePath string) ([]string, error) {
	fullPath := s.fullPath(remotePath)
	var objects []string

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(fullPath),
	})

	// Iterate over pages of results
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get page: %w", err)
		}

		for _, obj := range page.Contents {
			rel, err := filepath.Rel(s.prefix, *obj.Key)
			if err != nil {
				return nil, err
			}
			objects = append(objects, filepath.ToSlash(rel))
		}
	}

	return objects, nil
}

func (s *s3Storage) ListInfo(ctx context.Context, remotePath string) ([]FileInfo, error) {
	fullPath := s.fullPath(remotePath)
	var objects []FileInfo

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(fullPath),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get page: %w", err)
		}

		// Iterate over pages of results
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)

			// Normalize S3 keys using strings, not filepath
			rel := strings.TrimPrefix(key, s.prefix)
			rel = strings.TrimPrefix(rel, "/")

			objects = append(objects, FileInfo{
				Path:    filepath.ToSlash(rel),
				ModTime: aws.ToTime(obj.LastModified),
				Size:    aws.ToInt64(obj.Size),
			})
		}
	}

	return objects, nil
}

func (s *s3Storage) Delete(ctx context.Context, remotePath string) error {
	fullPath := s.fullPath(remotePath)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &fullPath,
	})
	return err
}

func (s *s3Storage) DeleteAll(ctx context.Context, remotePath string) error {
	return s.deleteAllVersions(ctx, remotePath)
}

func (s *s3Storage) DeleteDir(ctx context.Context, remotePath string) error {
	err := s.deleteAllVersions(ctx, remotePath)
	if err != nil {
		return err
	}
	return s.Delete(ctx, remotePath)
}

func (s *s3Storage) DeleteAllBulk(ctx context.Context, paths []string) error {
	return s.deleteAllVersionsBulk(ctx, paths)
}

func (s *s3Storage) deleteAllVersions(ctx context.Context, remotePath string) error {
	prefix := s.fullPath(remotePath)
	if prefix != "" && !endsWithSlash(prefix) {
		prefix += "/"
	}

	paginator := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{
		Bucket: &s.bucket,
		Prefix: &prefix,
	})

	var toDelete []s3types.ObjectIdentifier
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list object versions: %w", err)
		}

		for i := range page.Versions {
			version := page.Versions[i]
			toDelete = append(toDelete, s3types.ObjectIdentifier{
				Key:       version.Key,
				VersionId: version.VersionId,
			})
		}
		for _, marker := range page.DeleteMarkers {
			toDelete = append(toDelete, s3types.ObjectIdentifier{
				Key:       marker.Key,
				VersionId: marker.VersionId,
			})
		}
	}

	for i := 0; i < len(toDelete); i += 1000 {
		end := i + 1000
		if end > len(toDelete) {
			end = len(toDelete)
		}

		_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &s.bucket,
			Delete: &s3types.Delete{
				Objects: toDelete[i:end],
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("delete versions: %w", err)
		}
	}

	return nil
}

func (s *s3Storage) deleteAllVersionsBulk(ctx context.Context, paths []string) error {
	var objectsToDelete []s3types.ObjectIdentifier

	for _, path := range paths {
		prefix := s.fullPath(path)

		paginator := s3.NewListObjectVersionsPaginator(s.client, &s3.ListObjectVersionsInput{
			Bucket: &s.bucket,
			Prefix: &prefix,
		})

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("list object versions for %q: %w", prefix, err)
			}
			for i := range page.Versions {
				version := page.Versions[i]
				objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
					Key:       version.Key,
					VersionId: version.VersionId,
				})
			}
			for i := range page.DeleteMarkers {
				deleteMarker := page.DeleteMarkers[i]
				objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
					Key:       deleteMarker.Key,
					VersionId: deleteMarker.VersionId,
				})
			}
		}
	}

	// Split into chunks of 1000 due to S3 limit per DeleteObjects request
	const batchSize = 1000
	for i := 0; i < len(objectsToDelete); i += batchSize {
		end := i + batchSize
		if end > len(objectsToDelete) {
			end = len(objectsToDelete)
		}

		_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: &s.bucket,
			Delete: &s3types.Delete{
				Objects: objectsToDelete[i:end],
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("delete objects batch %d–%d: %w", i, end, err)
		}
	}

	return nil
}

func (s *s3Storage) Exists(ctx context.Context, remotePath string) (bool, error) {
	remotePath = s.fullPath(remotePath)

	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remotePath),
	})
	if err != nil {
		var nf *s3types.NotFound
		if errors.As(err, &nf) {
			return false, nil
		}
		return false, err
	}
	return true, nil // S3 has no dirs, so it's a valid file
}

func (s *s3Storage) ListTopLevelDirs(ctx context.Context, prefix string) (map[string]bool, error) {
	remotePath := s.fullPath(prefix)
	if !endsWithSlash(remotePath) {
		remotePath += "/"
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Delimiter: aws.String("/"), // Groups results by prefix (like top-level directories)
		Prefix:    aws.String(remotePath),
	}

	output, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects in bucket: %w", err)
	}

	// Extract top-level prefixes (directories)
	prefixes := make(map[string]bool)
	for _, prefix := range output.CommonPrefixes {
		if prefix.Prefix == nil {
			continue
		}
		prefixClean := strings.TrimSuffix(*prefix.Prefix, "/")
		rel, err := filepath.Rel(s.prefix, prefixClean)
		if err != nil {
			return nil, err
		}
		prefixes[filepath.ToSlash(rel)] = true
	}

	return prefixes, nil
}

func (s *s3Storage) Rename(ctx context.Context, oldRemotePath, newRemotePath string) error {
	srcKey := s.fullPath(oldRemotePath)
	dstKey := s.fullPath(newRemotePath)

	if srcKey == dstKey {
		return nil
	}

	// Copy source object to destination key
	copySource := s.bucket + "/" + srcKey

	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(dstKey),
	})
	if err != nil {
		return fmt.Errorf("copy object %q -> %q: %w", srcKey, dstKey, err)
	}

	// Delete source object (only latest version if bucket is versioned)
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return fmt.Errorf("delete source after copy %q: %w", srcKey, err)
	}

	return nil
}

func endsWithSlash(s string) bool {
	return s != "" && s[len(s)-1] == '/'
}

// multipart upload

func isSeekable(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	if !ok {
		return nil, false
	}
	return f, true
}

func (s *s3Storage) putMultipartStream(ctx context.Context, remotePath string, r io.Reader, partSize int64) error {
	if partSize < MinS3PartSize {
		partSize = MinS3PartSize
	}

	createOut, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remotePath),
	})
	if err != nil {
		return fmt.Errorf("create multipart upload %q: %w", remotePath, err)
	}

	uploadID := aws.ToString(createOut.UploadId)
	completedParts := make([]s3types.CompletedPart, 0, 128)

	abort := func(abortErr error) error {
		//nolint:errcheck
		_, _ = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(s.bucket),
			Key:      aws.String(remotePath),
			UploadId: aws.String(uploadID),
		})
		return abortErr
	}

	buf := make([]byte, partSize)
	var partNumber int32 = 1

	for {
		n, readErr := io.ReadFull(r, buf)

		switch {
		case readErr == nil:
			// full part
		case errors.Is(readErr, io.ErrUnexpectedEOF):
			// final partial part
		case errors.Is(readErr, io.EOF):
			// no more data
			n = 0
		default:
			return abort(fmt.Errorf("read source for %q: %w", remotePath, readErr))
		}

		if n > 0 {
			if int64(partNumber) > MaxS3UploadParts {
				return abort(fmt.Errorf(
					"multipart upload exceeded %d parts for %q; choose larger part size than %d bytes",
					MaxS3UploadParts, remotePath, partSize,
				))
			}

			upOut, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
				Bucket:        aws.String(s.bucket),
				Key:           aws.String(remotePath),
				UploadId:      aws.String(uploadID),
				PartNumber:    aws.Int32(partNumber),
				Body:          bytes.NewReader(buf[:n]),
				ContentLength: aws.Int64(int64(n)),
			})
			if err != nil {
				return abort(fmt.Errorf("upload part %d for %q: %w", partNumber, remotePath, err))
			}

			completedParts = append(completedParts, s3types.CompletedPart{
				ETag:       upOut.ETag,
				PartNumber: aws.Int32(partNumber),
			})

			partNumber++
		}

		if errors.Is(readErr, io.ErrUnexpectedEOF) || errors.Is(readErr, io.EOF) {
			break
		}
	}

	// empty object
	if len(completedParts) == 0 {
		_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(remotePath),
			Body:   bytes.NewReader(nil),
		})
		if err != nil {
			return abort(fmt.Errorf("put empty object %q: %w", remotePath, err))
		}
		return nil
	}

	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(remotePath),
		UploadId: aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return abort(fmt.Errorf("complete multipart upload %q: %w", remotePath, err))
	}

	return nil
}
