package xlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// FsyncFname fsyncs path contents and the parent directory contents.
func FsyncFname(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := FsyncDir(filepath.Dir(path)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// FsyncDir fsyncs dir contents.
func FsyncDir(dirPath string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("cannot open dir %s: %w", dirPath, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return fmt.Errorf("cannot fsync dir %s: %w", dirPath, err)
	}
	return d.Close()
}
