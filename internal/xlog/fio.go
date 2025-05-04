package xlog

import "os"

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// File does not exist or another error
		return false
	}
	return info.Mode().IsRegular()
}
