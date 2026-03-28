package storecrypt

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/sftp"
)

type sftpStorage struct {
	client  *sftp.Client
	baseDir string
}

var _ Storage = &sftpStorage{}

func NewSFTPStorage(client *sftp.Client, remoteDir string) Storage {
	return &sftpStorage{
		client:  client,
		baseDir: strings.TrimSuffix(remoteDir, "/"),
	}
}

func (s *sftpStorage) fullPath(p string) string {
	return filepath.ToSlash(filepath.Join(s.baseDir, filepath.Clean(p)))
}

func (s *sftpStorage) Put(_ context.Context, remotePath string, r io.Reader) error {
	fullPath := s.fullPath(remotePath)

	// Ensure directory exists
	dir := path.Dir(fullPath)
	if err := s.client.MkdirAll(dir); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Open file for writing
	f, err := s.client.Create(fullPath)
	if err != nil {
		return fmt.Errorf("sftp create: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	return err
}

func (s *sftpStorage) Get(_ context.Context, remotePath string) (io.ReadCloser, error) {
	fullPath := s.fullPath(remotePath)
	f, err := s.client.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("sftp open: %w", err)
	}
	return f, nil
}

func (s *sftpStorage) List(_ context.Context, remotePath string) ([]string, error) {
	fullPath := s.fullPath(remotePath)
	var result []string

	walker := s.client.Walk(fullPath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return nil, fmt.Errorf("error walking directory: %w", err)
		}
		stat := walker.Stat()
		if stat == nil {
			continue
		}
		if stat.IsDir() {
			continue
		}
		if walker.Path() != fullPath {
			rel, err := filepath.Rel(s.baseDir, walker.Path())
			if err != nil {
				return nil, err
			}
			result = append(result, rel)
		}
	}

	return result, nil
}

func (s *sftpStorage) ListInfo(_ context.Context, remotePath string) ([]FileInfo, error) {
	fullPath := s.fullPath(remotePath)
	var result []FileInfo

	walker := s.client.Walk(fullPath)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return nil, fmt.Errorf("error walking directory: %w", err)
		}
		stat := walker.Stat()
		if stat == nil {
			continue
		}
		if stat.IsDir() {
			continue
		}
		if walker.Path() != fullPath {
			rel, err := filepath.Rel(s.baseDir, walker.Path())
			if err != nil {
				return nil, err
			}
			result = append(result, FileInfo{
				Path:    rel,
				ModTime: stat.ModTime(),
				Size:    stat.Size(),
			})
		}
	}

	return result, nil
}

func (s *sftpStorage) Delete(_ context.Context, remotePath string) error {
	return s.client.Remove(s.fullPath(remotePath))
}

func (s *sftpStorage) DeleteDir(_ context.Context, remotePath string) error {
	return s.client.RemoveAll(s.fullPath(remotePath))
}

func (s *sftpStorage) DeleteAll(_ context.Context, remotePath string) error {
	fullPath := s.fullPath(remotePath)

	entries, err := s.client.ReadDir(fullPath)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "file does not exist") {
			return nil
		}
		return fmt.Errorf("reading directory %q: %w", fullPath, err)
	}

	for _, entry := range entries {
		pathToRemove := path.Join(fullPath, entry.Name())
		err := s.client.RemoveAll(pathToRemove)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "file does not exist") {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *sftpStorage) DeleteAllBulk(_ context.Context, paths []string) error {
	for i := range paths {
		fullPath := s.fullPath(paths[i])
		err := s.client.RemoveAll(fullPath)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "file does not exist") {
				return nil
			}
			return err
		}
	}
	return nil
}

func (s *sftpStorage) Exists(_ context.Context, remotePath string) (bool, error) {
	fullPath := s.fullPath(remotePath)
	info, err := s.client.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func (s *sftpStorage) ListTopLevelDirs(_ context.Context, prefix string) (map[string]bool, error) {
	fullPath := s.fullPath(prefix)
	result := make(map[string]bool)

	entries, err := s.client.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirFullPath := filepath.ToSlash(filepath.Join(fullPath, entry.Name()))
			rel, err := filepath.Rel(s.baseDir, dirFullPath)
			if err != nil {
				return nil, err
			}
			result[filepath.ToSlash(rel)] = true
		}
	}
	return result, nil
}

func (s *sftpStorage) Rename(_ context.Context, oldRemotePath, newRemotePath string) error {
	oldFull := s.fullPath(oldRemotePath)
	newFull := s.fullPath(newRemotePath)

	if oldFull == newFull {
		return nil
	}

	// Ensure destination directory exists
	dir := path.Dir(newFull)
	if err := s.client.MkdirAll(dir); err != nil {
		return fmt.Errorf("mkdir dest dir %q: %w", dir, err)
	}

	if err := s.client.Rename(oldFull, newFull); err != nil {
		return fmt.Errorf("sftp rename %q -> %q: %w", oldFull, newFull, err)
	}

	return nil
}
