package fsx

import (
	"fmt"
	"os"
)

func ByteCountIEC(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB",
		float64(b)/float64(div), "KMGTPE"[exp])
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// File does not exist or another error
		return false
	}
	return info.Mode().IsRegular()
}

func DirExistsAndNotEmpty(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Directory does not exist
		}
		return false, err // Other error
	}

	if !info.IsDir() {
		return false, fmt.Errorf("path exists but is not a directory")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}

	return len(entries) > 0, nil
}

func isSymlinkTo(oldName, newName string) bool {
	file, err := os.Stat(newName)
	if err != nil {
		return false
	}
	if file.Mode()&os.ModeSymlink != os.ModeSymlink {
		return false
	}
	target, err := os.Readlink(newName)
	if err != nil {
		return false
	}
	return target == oldName
}

func EnsureSymlink(oldName, newName string) error {
	if isSymlinkTo(oldName, newName) {
		return nil
	}
	_ = os.Remove(newName)
	return os.Symlink(oldName, newName)
}
