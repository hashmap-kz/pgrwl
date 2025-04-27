package xlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// FsyncFile fsyncs path contents and the parent directory contents.
func FsyncFname(path string) error {
	if err := fsync(path); err != nil {
		return fmt.Errorf("cannot fsync file %q: %s", path, err)
	}
	if runtime.GOOS != "windows" {
		dir := filepath.Dir(path)
		if err := fsync(dir); err != nil {
			return fmt.Errorf("cannot fsync dir %q: %s", dir, err)
		}
	}
	return nil
}

func fsync(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
