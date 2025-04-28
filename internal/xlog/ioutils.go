package xlog

import (
	"os"
)

// FsyncFname fsyncs path contents and the parent directory contents.
func FsyncFname(path string) error {
	// if err := fsync(path); err != nil {
	// 	return fmt.Errorf("cannot fsync file %s: %s", filepath.ToSlash(path), err)
	// }
	// if runtime.GOOS != "windows" {
	// 	dir := filepath.Dir(path)
	// 	if err := fsync(dir); err != nil {
	// 		return fmt.Errorf("cannot fsync dir %q: %s", dir, err)
	// 	}
	// }

	// TODO:fix
	return nil
}

// FsyncDir fsyncs dir contents.
func FsyncDir(dir string) error {
	// if runtime.GOOS == "windows" {
	// 	return nil
	// }
	// return fsync(dir)

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
