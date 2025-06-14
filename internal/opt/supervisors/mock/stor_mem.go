package mock

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

// InMemoryStorage is a simple in-memory implementation of the storecrypt.Storage interface.
type InMemoryStorage struct {
	Files map[string][]byte
	mu    sync.RWMutex
}

var _ storage.Storage = &InMemoryStorage{}

func NewInMemoryStorage() *InMemoryStorage {
	return &InMemoryStorage{
		Files: make(map[string][]byte),
	}
}

func (s *InMemoryStorage) Put(_ context.Context, path string, r io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.Files[path] = data
	return nil
}

func (s *InMemoryStorage) Get(_ context.Context, path string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, ok := s.Files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *InMemoryStorage) Delete(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.Files[path]; !ok {
		return errors.New("file not found")
	}
	delete(s.Files, path)
	return nil
}

func (s *InMemoryStorage) DeleteAll(ctx context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prefix := strings.TrimSuffix(path, "/") + "/"

	for key := range s.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if strings.HasPrefix(key, prefix) || key == path {
			delete(s.Files, key)
		}
	}

	return nil
}

func (s *InMemoryStorage) DeleteAllBulk(ctx context.Context, paths []string) error {
	for _, p := range paths {
		if err := s.DeleteAll(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryStorage) List(_ context.Context, path string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := strings.TrimSuffix(path, "/") + "/"

	keys := make([]string, 0)
	for k := range s.Files {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (s *InMemoryStorage) Exists(_ context.Context, path string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.Files[path]
	return ok, nil
}

func (s *InMemoryStorage) ListInfo(_ context.Context, path string) ([]storage.FileInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var infos []storage.FileInfo
	prefix := strings.TrimSuffix(path, "/") + "/"

	for name := range s.Files {
		if strings.HasPrefix(name, prefix) {
			infos = append(infos, storage.FileInfo{
				Path:    name,
				ModTime: time.Now(),
			})
		}
	}
	return infos, nil
}

func (s *InMemoryStorage) ListTopLevelDirs(_ context.Context, _ string) (map[string]bool, error) {
	// TODO implement me
	panic("implement me")
}
