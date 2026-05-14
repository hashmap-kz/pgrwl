package storecrypt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/pgrwl/pgrwl/internal/core/logger"
)

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

			log.LogAttrs(ctx, logger.LevelTrace, "uploading multipart chunk",
				slog.Int64("part_number", int64(partNumber)),
				slog.Int("chunk_size_bytes", n),
			)

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

			log.LogAttrs(ctx, logger.LevelTrace, "multipart chunk uploaded",
				slog.Int64("part_number", int64(partNumber)),
				slog.Int("chunk_size_bytes", n),
			)

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

	log.LogAttrs(ctx, logger.LevelTrace, "multipart upload completed",
		slog.Int("parts", len(completedParts)),
	)

	return nil
}
