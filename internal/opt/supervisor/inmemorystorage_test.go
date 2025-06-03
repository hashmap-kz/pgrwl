package supervisor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/hashmap-kz/storecrypt/pkg/storage"
)

// InMemoryStorage is a simple in-memory implementation of the storage.Storage interface.
type InMemoryStorage struct {
	files map[string][]byte
	mu    sync.RWMutex
}

func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		files: make(map[string][]byte),
	}
}

// Put stores a file from the reader to the destination path.
func (s *InMemoryStorage) Put(_ context.Context, path string, r io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.files[path] = data
	return nil
}

// Get retrieves a file as a stream. Caller must close the reader.
func (s *InMemoryStorage) Get(_ context.Context, path string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// List returns all file names under the given directory.
func (s *InMemoryStorage) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.files[path]; !ok {
		return errors.New("file not found")
	}
	delete(s.files, path)
	return nil
}

// DeleteAll removes all files and directories in a specified path.
func (s *InMemoryStorage) DeleteAll(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Normalize the path with a trailing slash to avoid partial matches
	prefix := strings.TrimSuffix(path, "/") + "/"

	for key := range s.files {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasPrefix(key, prefix) || key == path {
			delete(s.files, key)
		}
	}

	return nil
}

// DeleteAllBulk removes all files and directories in a specified list of paths.
func (s *InMemoryStorage) DeleteAllBulk(ctx context.Context, paths []string) error {
	for _, p := range paths {
		if err := s.DeleteAll(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// List returns all file names under the given directory.
func (s *InMemoryStorage) List(_ context.Context, path string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Normalize path with a trailing slash to match directories
	prefix := strings.TrimSuffix(path, "/") + "/"

	keys := make([]string, 0)
	for k := range s.files {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Exists checks whether a file exists.
func (s *InMemoryStorage) Exists(_ context.Context, path string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.files[path]
	return ok, nil
}

// ListInfo returns all file infos under the given directory.
func (s *InMemoryStorage) ListInfo(_ context.Context, path string) ([]storage.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var infos []storage.FileInfo
	prefix := strings.TrimSuffix(path, "/") + "/"

	for name := range s.files {
		if strings.HasPrefix(name, prefix) {
			infos = append(infos, storage.FileInfo{
				Path:    name,
				ModTime: time.Now(),
			})
		}
	}
	return infos, nil
}

// Make sure InMemoryStorage implements storage.Storage interface
var _ storage.Storage = (*InMemoryStorage)(nil)
