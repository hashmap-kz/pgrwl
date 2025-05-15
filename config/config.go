//go:generate go run ../cmd/genmerge/main.go

package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

var (
	once   sync.Once
	config *Config
)

type Config struct {
	StorageType            string `json:"PGRWL_STORAGE_TYPE,omitempty"`
	StorageCompressionAlgo string `json:"PGRWL_STORAGE_COMPRESSION_ALGO,omitempty"`
	StorageEncryptionAlgo  string `json:"PGRWL_STORAGE_ENCRYPTION_ALGO,omitempty"`
	StorageEncryptionPass  string `json:"PGRWL_STORAGE_ENCRYPTION_PASS,omitempty"`
	SFTPHost               string `json:"PGRWL_SFTP_HOST,omitempty"`
	SFTPPort               int    `json:"PGRWL_SFTP_PORT,omitempty"`
	SFTPUser               string `json:"PGRWL_SFTP_USER,omitempty"`
	SFTPPass               string `json:"PGRWL_SFTP_PASS,omitempty"`
	SFTPPkeyPath           string `json:"PGRWL_SFTP_PKEY_PATH,omitempty"`
	SFTPPkeyPass           string `json:"PGRWL_SFTP_PKEY_PASS,omitempty"`
	S3URL                  string `json:"PGRWL_S3_URL,omitempty"`
	S3AccessKeyID          string `json:"PGRWL_S3_ACCESS_KEY_ID,omitempty"`
	S3SecretAccessKey      string `json:"PGRWL_S3_SECRET_ACCESS_KEY,omitempty"`
	S3Bucket               string `json:"PGRWL_S3_BUCKET,omitempty"`
	S3Region               string `json:"PGRWL_S3_REGION,omitempty"`
	S3UsePathStyle         bool   `json:"PGRWL_S3_USE_PATH_STYLE,omitempty"`
	S3DisableSSL           bool   `json:"PGRWL_S3_DISABLE_SSL,omitempty"`
}

func (c *Config) String() string {
	// Step 1: Make a shallow copy
	cp := *c

	// Step 2: Redact sensitive fields (distinct between empty and filled)
	const redacted = "[REDACTED]"
	if cp.StorageEncryptionPass != "" {
		cp.StorageEncryptionPass = redacted
	}
	if cp.SFTPPass != "" {
		cp.SFTPPass = redacted
	}
	if cp.SFTPPkeyPass != "" {
		cp.SFTPPkeyPass = redacted
	}
	if cp.S3SecretAccessKey != "" {
		cp.S3SecretAccessKey = redacted
	}

	// Step 3: Marshal the copy
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func Cfg() *Config {
	if config == nil {
		log.Fatal("config was not loaded in main")
	}
	return config
}

func Read(path string) *Config {
	once.Do(func() {
		var err error
		config, err = mustLoadCfg(path)
		if err != nil {
			log.Fatal(err)
		}
	})
	return config
}

func mustLoadCfg(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	// fill from env if JSON did not provide values
	mergeEnvIfUnset(&c)
	return &c, nil
}
