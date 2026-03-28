package storecrypt

import (
	"context"
	"io"
	"time"
)

type FileInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// Storage is an interface for handling remote file storage.
type Storage interface {
	// Put stores a file from the reader to the destination path.
	Put(ctx context.Context, remotePath string, r io.Reader) error

	// Get retrieves a remote file as a stream. Caller must close the reader.
	Get(ctx context.Context, remotePath string) (io.ReadCloser, error)

	// List returns all file names under the given directory.
	List(ctx context.Context, remotePath string) ([]string, error)

	// ListInfo returns all file infos under the given directory.
	ListInfo(ctx context.Context, remotePath string) ([]FileInfo, error)

	// Delete removes the specified file.
	Delete(ctx context.Context, remotePath string) error

	// DeleteAll removes all files and directories in a specified path.
	DeleteAll(ctx context.Context, remotePath string) error

	// DeleteDir removes a directory (or prefix) with its content.
	DeleteDir(ctx context.Context, remotePath string) error

	// DeleteAllBulk removes all files and directories in a specified list of paths.
	DeleteAllBulk(ctx context.Context, paths []string) error

	// Exists checks whether a file exists.
	Exists(ctx context.Context, remotePath string) (bool, error)

	// ListTopLevelDirs retrieves ONLY directories at a given prefix path.
	ListTopLevelDirs(ctx context.Context, prefix string) (map[string]bool, error)

	// Rename moves/renames a single object from oldRemotePath to newRemotePath.
	// For S3 this is implemented as copy+delete (not recursive prefix rename).
	Rename(ctx context.Context, oldRemotePath, newRemotePath string) error
}
