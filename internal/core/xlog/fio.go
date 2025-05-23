package xlog

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// File does not exist or another error
		return false
	}
	return info.Mode().IsRegular()
}

//nolint:unused
func sha256Path(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()

	// Stream file into hash
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
