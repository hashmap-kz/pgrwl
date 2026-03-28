package storecrypt

import (
	"context"
	"io"
	"log/slog"
)

type loggingStorage struct {
	inner Storage
}

// WithLogging wraps a Storage with debug logging for each operation.
func WithLogging(inner Storage) Storage {
	return &loggingStorage{inner: inner}
}

func (l *loggingStorage) Put(ctx context.Context, remotePath string, r io.Reader) error {
	slog.Debug("storage: Put", "path", remotePath)
	err := l.inner.Put(ctx, remotePath, r)
	if err != nil {
		slog.Error("storage: Put failed", "path", remotePath, "error", err)
	}
	return err
}

func (l *loggingStorage) Get(ctx context.Context, remotePath string) (io.ReadCloser, error) {
	slog.Debug("storage: Get", "path", remotePath)
	rc, err := l.inner.Get(ctx, remotePath)
	if err != nil {
		slog.Error("storage: Get failed", "path", remotePath, "error", err)
	}
	return rc, err
}

func (l *loggingStorage) List(ctx context.Context, remotePath string) ([]string, error) {
	slog.Debug("storage: List", "path", remotePath)
	res, err := l.inner.List(ctx, remotePath)
	if err != nil {
		slog.Error("storage: List failed", "path", remotePath, "error", err)
	}
	return res, err
}

func (l *loggingStorage) ListInfo(ctx context.Context, remotePath string) ([]FileInfo, error) {
	slog.Debug("storage: ListInfo", "path", remotePath)
	res, err := l.inner.ListInfo(ctx, remotePath)
	if err != nil {
		slog.Error("storage: ListInfo failed", "path", remotePath, "error", err)
	}
	return res, err
}

func (l *loggingStorage) Delete(ctx context.Context, remotePath string) error {
	slog.Debug("storage: Delete", "path", remotePath)
	err := l.inner.Delete(ctx, remotePath)
	if err != nil {
		slog.Error("storage: Delete failed", "path", remotePath, "error", err)
	}
	return err
}

func (l *loggingStorage) DeleteAll(ctx context.Context, remotePath string) error {
	slog.Debug("storage: DeleteAll", "path", remotePath)
	err := l.inner.DeleteAll(ctx, remotePath)
	if err != nil {
		slog.Error("storage: DeleteAll failed", "path", remotePath, "error", err)
	}
	return err
}

func (l *loggingStorage) DeleteDir(ctx context.Context, remotePath string) error {
	slog.Debug("storage: DeleteDir", "path", remotePath)
	err := l.inner.DeleteDir(ctx, remotePath)
	if err != nil {
		slog.Error("storage: DeleteDir failed", "path", remotePath, "error", err)
	}
	return err
}

func (l *loggingStorage) DeleteAllBulk(ctx context.Context, paths []string) error {
	slog.Debug("storage: DeleteAllBulk", "count", len(paths))
	err := l.inner.DeleteAllBulk(ctx, paths)
	if err != nil {
		slog.Error("storage: DeleteAllBulk failed", "error", err)
	}
	return err
}

func (l *loggingStorage) Exists(ctx context.Context, remotePath string) (bool, error) {
	slog.Debug("storage: Exists", "path", remotePath)
	exists, err := l.inner.Exists(ctx, remotePath)
	if err != nil {
		slog.Error("storage: Exists failed", "path", remotePath, "error", err)
	}
	return exists, err
}

func (l *loggingStorage) ListTopLevelDirs(ctx context.Context, prefix string) (map[string]bool, error) {
	slog.Debug("storage: ListTopLevelDirs", "prefix", prefix)
	res, err := l.inner.ListTopLevelDirs(ctx, prefix)
	if err != nil {
		slog.Error("storage: ListTopLevelDirs failed", "prefix", prefix, "error", err)
	}
	return res, err
}

func (l *loggingStorage) Rename(ctx context.Context, oldRemotePath, newRemotePath string) error {
	slog.Debug("storage: Rename", "old", oldRemotePath, "new", newRemotePath)
	err := l.inner.Rename(ctx, oldRemotePath, newRemotePath)
	if err != nil {
		slog.Error("storage: Rename failed", "old", oldRemotePath, "new", newRemotePath, "error", err)
	}
	return err
}
