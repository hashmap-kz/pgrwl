package storecrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pgrwl/pgrwl/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pgrwl/pgrwl/internal/core/logger"
)

const (
	MinS3PartSize     int64 = 5 * 1024 * 1024
	MaxS3PartSize     int64 = 5 * 1024 * 1024 * 1024
	DefaultS3PartSize int64 = 16 * 1024 * 1024
	DefaultS3Conc           = 2
	MaxS3Conc               = 16
	MaxS3UploadParts  int64 = 10000

	// MultipartDefaultPartSizeBytes is used for large unknown-size streams
	// such as base backups. 256 MiB x 10000 parts = ~2.44 TiB max object.
	MultipartDefaultPartSizeBytes = 256 * 1024 * 1024

	MultipartAbortTimeout = 30 * time.Second
)

type S3Options struct {
	PartSizeBytes int64
	Concurrency   int
	Log           *slog.Logger
}

type s3Storage struct {
	client         *s3.Client
	bucket         string
	prefix         string
	streamPartSize int64 // part size for the streaming multipart path
	concurrency    int
	log            *slog.Logger
}

var _ Storage = &s3Storage{}

func NewS3Storage(client *s3.Client, bucket, prefix string) Storage {
	return NewS3StorageWithOptions(client, bucket, prefix, S3Options{})
}

func NewS3StorageWithOptions(client *s3.Client, bucket, prefix string, opts S3Options) Storage {
	// streamPartSize: the part size used when the reader has unknown size
	// (e.g. after compression/encryption wraps it in an io.Pipe).
	streamPartSize := opts.PartSizeBytes
	if streamPartSize <= 0 {
		streamPartSize = MultipartDefaultPartSizeBytes
	}
	streamPartSize = normalizeS3PartSize(streamPartSize)

	return &s3Storage{
		client:         client,
		bucket:         bucket,
		prefix:         filepath.ToSlash(strings.TrimPrefix(prefix, "/")),
		streamPartSize: streamPartSize,
		concurrency:    normalizeConcurrency(opts.Concurrency),
		log:            opts.Log,
	}
}

func normalizeS3PartSize(partSize int64) int64 {
	if partSize <= 0 {
		return DefaultS3PartSize
	}
	if partSize < MinS3PartSize {
		return MinS3PartSize
	}
	if partSize > MaxS3PartSize {
		return MaxS3PartSize
	}
	return partSize
}

func normalizeConcurrency(c int) int {
	if c <= 0 {
		return DefaultS3Conc
	}
	if c > MaxS3Conc {
		return MaxS3Conc
	}
	return c
}

func (s *s3Storage) fullPath(path string) string {
	return filepath.ToSlash(filepath.Join(s.prefix, path))
}

func (s *s3Storage) logf() *slog.Logger {
	if s.log != nil {
		return s.log.With(
			slog.String("component", "storage-s3"),
			slog.String("bucket", s.bucket),
		)
	}
	return slog.Default().With(
		slog.String("component", "storage-s3"),
		slog.String("bucket", s.bucket),
	)
}

// CreateUploader creates a new S3 uploader with the given part size and concurrency.
func CreateUploader(client *s3.Client, partSize int64, concurrency int) *transfermanager.Client {
	return transfermanager.New(client, func(o *transfermanager.Options) {
		o.PartSizeBytes = normalizeS3PartSize(partSize)
		o.Concurrency = normalizeConcurrency(concurrency)
	})
}

// ChooseUploadPartSize returns a safe part size for a known or estimated object size.
// If size <= 0, it returns the default known-size upload part size.
func ChooseUploadPartSize(size int64) int64 {
	if size <= 0 {
		return DefaultS3PartSize
	}

	// Examples:
	//
	// object-size = 16Mi = 1048576 bytes
	// (1048576 + 10000 - 1) / 10000 = 106 bytes
	//
	// object-size = 50GiB = 53687091200 bytes
	// (53687091200 + 10000 - 1) / 10000 = 5368710 bytes = ~5.12MiB
	//
	// object-size = 500GiB = 536870912000 bytes
	// (536870912000 + 10000 - 1) / 10000 = 5368710 bytes = ~51.2MiB
	partSize := (size + MaxS3UploadParts - 1) / MaxS3UploadParts
	if partSize < MinS3PartSize {
		partSize = MinS3PartSize
	}

	// round up to whole MiB for cleaner values
	const mib = int64(1024 * 1024)
	if rem := partSize % mib; rem != 0 {
		partSize += mib - rem
	}

	return normalizeS3PartSize(partSize)
}

func (s *s3Storage) Put(ctx context.Context, remotePath string, r io.Reader) error {
	fullPath := s.fullPath(remotePath)

	log := s.logf().With(
		slog.String("path", remotePath),
		slog.String("s3_key", fullPath),
	)

	// If we know the size, use transfermanager with computed part size.
	if f, ok := isSeekable(r); ok {
		st, err := f.Stat()
		if err == nil {
			size := st.Size()
			partSize := ChooseUploadPartSize(size)
			uploader := CreateUploader(s.client, partSize, s.concurrency)

			log.Debug("using seekable upload path",
				slog.Int64("size_bytes", size),
				slog.Int64("part_size_bytes", partSize),
				slog.Int("concurrency", s.concurrency),
			)

			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("seek file for %q: %w", fullPath, err)
			}

			_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
				Bucket: aws.String(s.bucket),
				Key:    aws.String(fullPath),
				Body:   f,
			})
			if err != nil {
				return fmt.Errorf("s3 upload %q: %w", fullPath, err)
			}
			return nil
		}
	}

	log.Debug("using streaming multipart upload path",
		slog.Int64("part_size_bytes", s.streamPartSize),
	)

	// Unknown-size stream: use manual multipart upload.
	// Part size is configured per-instance: large for backups (256 MiB),
	// small for WAL segments (16 MiB). See WALPartSizeBytes / MultipartDefaultPartSizeBytes.
	return s.putMultipartStream(ctx, fullPath, r, s.streamPartSize)
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
			rel := strings.TrimPrefix(aws.ToString(obj.Key), s.prefix)
			rel = strings.TrimPrefix(rel, "/")
			objects = append(objects, rel)
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
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullPath),
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
			return fmt.Errorf("delete objects batch %d-%d: %w", i, end, err)
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

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.bucket),
		Delimiter: aws.String("/"), // Groups results by prefix (like top-level directories)
		Prefix:    aws.String(remotePath),
	})

	// Extract top-level prefixes (directories)
	prefixes := make(map[string]bool)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects in bucket: %w", err)
		}
		for _, prefix := range page.CommonPrefixes {
			if prefix.Prefix == nil {
				continue
			}
			prefixClean := strings.TrimSuffix(*prefix.Prefix, "/")
			rel := strings.TrimPrefix(prefixClean, s.prefix)
			rel = strings.TrimPrefix(rel, "/")
			prefixes[rel] = true
		}
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

func (s *s3Storage) abortMultipartUpload(remotePath, uploadID string) {
	// NOTE: dedicated context used on purpose
	ctx, cancel := context.WithTimeout(context.Background(), MultipartAbortTimeout)
	defer cancel()

	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(remotePath),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		s.logf().Warn("failed to abort multipart upload",
			slog.String("s3_key", remotePath),
			slog.String("upload_id", uploadID),
			slog.Any("error", err),
		)
	}
}

func (s *s3Storage) putMultipartStream(ctx context.Context, remotePath string, r io.Reader, partSize int64) error {
	partSize = normalizeS3PartSize(partSize)

	log := s.logf().With(
		slog.String("s3_key", remotePath),
		slog.Int64("part_size_bytes", partSize),
	)

	createOut, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(remotePath),
	})
	if err != nil {
		return fmt.Errorf("create multipart upload %q: %w", remotePath, err)
	}

	uploadID := aws.ToString(createOut.UploadId)
	completedParts := make([]s3types.CompletedPart, 0, 128)

	abortOnError := func(cause error) error {
		s.abortMultipartUpload(remotePath, uploadID)
		return cause
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
			return abortOnError(fmt.Errorf("read source for %q: %w", remotePath, readErr))
		}

		if n > 0 {
			if int64(partNumber) > MaxS3UploadParts {
				return abortOnError(fmt.Errorf(
					"multipart upload exceeded %d parts for %q; choose larger part size than %d bytes",
					MaxS3UploadParts, remotePath, partSize,
				))
			}

			if config.Verbose {
				log.LogAttrs(ctx, logger.LevelTrace, "uploading multipart chunk",
					slog.Int64("part_number", int64(partNumber)),
					slog.Int("chunk_size_bytes", n),
				)
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
				return abortOnError(fmt.Errorf("upload part %d for %q: %w", partNumber, remotePath, err))
			}

			completedParts = append(completedParts, s3types.CompletedPart{
				ETag:       upOut.ETag,
				PartNumber: aws.Int32(partNumber),
			})

			if config.Verbose {
				log.LogAttrs(ctx, logger.LevelTrace, "multipart chunk uploaded",
					slog.Int64("part_number", int64(partNumber)),
					slog.Int("chunk_size_bytes", n),
				)
			}

			partNumber++
		}

		if errors.Is(readErr, io.ErrUnexpectedEOF) || errors.Is(readErr, io.EOF) {
			break
		}
	}

	// empty object
	if len(completedParts) == 0 {
		// We created a multipart upload, but S3 cannot complete a multipart
		// upload with zero parts. Abort it first, then store the empty object
		// with regular PutObject.
		s.abortMultipartUpload(remotePath, uploadID)

		_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(remotePath),
			Body:   bytes.NewReader(nil),
		})
		if err != nil {
			return fmt.Errorf("put empty object %q: %w", remotePath, err)
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
		return abortOnError(fmt.Errorf("complete multipart upload %q: %w", remotePath, err))
	}

	if config.Verbose {
		log.LogAttrs(ctx, logger.LevelTrace, "multipart upload completed",
			slog.Int("parts", len(completedParts)),
		)
	}

	return nil
}
