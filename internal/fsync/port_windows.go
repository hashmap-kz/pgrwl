//go:build windows
// +build windows

package fsync

import (
	"os"
	"path/filepath"
	"syscall"
)

//nolint:revive
func Fsync(f *os.File) error {
	return syscall.FlushFileBuffers(syscall.Handle(f.Fd()))
}

// FsyncFname fsyncs path contents and the parent directory contents.
//
//nolint:revive
func FsyncFname(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := Fsync(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// FsyncDir fsyncs dir contents.
func FsyncDir(dirPath string) error {
	return nil
}

// FsyncFnameAndDir fsyncs the file by its path, and the parent dir
//
//nolint:revive
func FsyncFnameAndDir(fname string) error {
	if err := FsyncFname(fname); err != nil {
		return err
	}
	return FsyncDir(filepath.Dir(fname))
}
