package optutils

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
