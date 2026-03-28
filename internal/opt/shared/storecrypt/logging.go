package storecrypt

import (
	"context"
	"io"
	"log/slog"
)

type loggingStorage struct {
	l     *slog.Logger
	inner Storage
}

var _ Storage = (*loggingStorage)(nil)

// WithLogging wraps a Storage with debug logging for each operation.
func WithLogging(inner Storage, logger ...*slog.Logger) Storage {
	ls := &loggingStorage{inner: inner}
	if len(logger) > 0 && logger[0] != nil {
		ls.l = logger[0]
	}
	return ls
}

func (l *loggingStorage) log() *slog.Logger {
	if l.l != nil {
		return l.l.With(slog.String("component", "storage"))
	}
	return slog.Default().With(slog.String("component", "storage"))
}

func (l *loggingStorage) Put(ctx context.Context, remotePath string, r io.Reader) error {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("uploading object")

	err := l.inner.Put(ctx, remotePath, r)
	if err != nil {
		log.Error("object upload failed", slog.Any("err", err))
		return err
	}

	log.Debug("object uploaded")
	return nil
}

func (l *loggingStorage) Get(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("downloading object")

	rc, err := l.inner.Get(ctx, remotePath)
	if err != nil {
		log.Error("object download failed", slog.Any("err", err))
		return nil, err
	}

	log.Debug("object download stream opened")
	return rc, nil
}

func (l *loggingStorage) List(ctx context.Context, remotePath string) ([]string, error) {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("listing objects")

	res, err := l.inner.List(ctx, remotePath)
	if err != nil {
		log.Error("object listing failed", slog.Any("err", err))
		return nil, err
	}

	log.Debug("objects listed", slog.Int("count", len(res)))
	return res, nil
}

func (l *loggingStorage) ListInfo(ctx context.Context, remotePath string) ([]FileInfo, error) {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("listing objects with metadata")

	res, err := l.inner.ListInfo(ctx, remotePath)
	if err != nil {
		log.Error("object metadata listing failed", slog.Any("err", err))
		return nil, err
	}

	log.Debug("object metadata listed", slog.Int("count", len(res)))
	return res, nil
}

func (l *loggingStorage) Delete(ctx context.Context, remotePath string) error {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("deleting object")

	err := l.inner.Delete(ctx, remotePath)
	if err != nil {
		log.Error("object deletion failed", slog.Any("err", err))
		return err
	}

	log.Debug("object deleted")
	return nil
}

func (l *loggingStorage) DeleteAll(ctx context.Context, remotePath string) error {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("deleting all matching objects")

	err := l.inner.DeleteAll(ctx, remotePath)
	if err != nil {
		log.Error("bulk object deletion failed", slog.Any("err", err))
		return err
	}

	log.Debug("all matching objects deleted")
	return nil
}

func (l *loggingStorage) DeleteDir(ctx context.Context, remotePath string) error {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("deleting directory")

	err := l.inner.DeleteDir(ctx, remotePath)
	if err != nil {
		log.Error("directory deletion failed", slog.Any("err", err))
		return err
	}

	log.Debug("directory deleted")
	return nil
}

func (l *loggingStorage) DeleteAllBulk(ctx context.Context, paths []string) error {
	log := l.log().With(slog.Int("count", len(paths)))

	log.Debug("deleting objects in bulk")

	err := l.inner.DeleteAllBulk(ctx, paths)
	if err != nil {
		log.Error("bulk delete failed", slog.Any("err", err))
		return err
	}

	log.Debug("bulk delete completed")
	return nil
}

func (l *loggingStorage) Exists(ctx context.Context, remotePath string) (bool, error) {
	log := l.log().With(slog.String("path", remotePath))

	log.Debug("checking object existence")

	exists, err := l.inner.Exists(ctx, remotePath)
	if err != nil {
		log.Error("object existence check failed", slog.Any("err", err))
		return false, err
	}

	log.Debug("object existence checked", slog.Bool("exists", exists))
	return exists, nil
}

func (l *loggingStorage) ListTopLevelDirs(ctx context.Context, prefix string) (map[string]bool, error) {
	log := l.log().With(slog.String("prefix", prefix))

	log.Debug("listing top-level directories")

	res, err := l.inner.ListTopLevelDirs(ctx, prefix)
	if err != nil {
		log.Error("top-level directory listing failed", slog.Any("err", err))
		return nil, err
	}

	log.Debug("top-level directories listed", slog.Int("count", len(res)))
	return res, nil
}

func (l *loggingStorage) Rename(ctx context.Context, oldRemotePath, newRemotePath string) error {
	log := l.log().With(
		slog.String("old_path", oldRemotePath),
		slog.String("new_path", newRemotePath),
	)

	log.Debug("renaming object")

	err := l.inner.Rename(ctx, oldRemotePath, newRemotePath)
	if err != nil {
		log.Error("object rename failed", slog.Any("err", err))
		return err
	}

	log.Debug("object renamed")
	return nil
}
