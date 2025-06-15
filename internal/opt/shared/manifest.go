package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashmap-kz/pgrwl/config"
)

type StorageManifest struct {
	CompressionAlgo string `json:"compression_algo,omitempty"`
	EncryptionAlgo  string `json:"encryption_algo,omitempty"`
}

func CheckManifest(cfg *config.Config) error {
	manifest, err := readOrWriteManifest(cfg)
	if err != nil {
		return err
	}
	if manifest.CompressionAlgo != cfg.Storage.Compression.Algo {
		return fmt.Errorf("storage compression mismatch from previous setup")
	}
	if manifest.EncryptionAlgo != cfg.Storage.Encryption.Algo {
		return fmt.Errorf("storage encryption mismatch from previous setup")
	}
	return nil
}

func readOrWriteManifest(cfg *config.Config) (*StorageManifest, error) {
	var m StorageManifest
	manifestPath := filepath.Join(cfg.Main.Directory, ".manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		// create if not exists
		if errors.Is(err, os.ErrNotExist) {
			m.CompressionAlgo = cfg.Storage.Compression.Algo
			m.EncryptionAlgo = cfg.Storage.Encryption.Algo
			data, err := json.Marshal(&m)
			if err != nil {
				return nil, err
			}
			err = os.WriteFile(manifestPath, data, 0o600)
			if err != nil {
				return nil, err
			}
			return &m, nil
		}
		return nil, err
	}
	err = json.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
