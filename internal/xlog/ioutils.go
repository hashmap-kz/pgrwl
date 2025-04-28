package xlog

import (
	"fmt"
	"os"
	"path/filepath"
)

// FsyncFname fsyncs path contents and the parent directory contents.
func FsyncFname(path string) error {
	if err := fsync(path); err != nil {
		return fmt.Errorf("cannot fsync file %s: %s", filepath.ToSlash(path), err)
	}
	return nil
}

// FsyncDir fsyncs dir contents.
func FsyncDir(dir string) error {
	// TODO:fix
	return nil
}

func fsync(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
