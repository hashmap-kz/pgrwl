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
	Mode         string `json:"PGRWL_MODE"`
	Directory    string `json:"PGRWL_DIRECTORY"`
	Slot         string `json:"PGRWL_SLOT"`
	NoLoop       bool   `json:"PGRWL_NO_LOOP"`
	ListenPort   int    `json:"PGRWL_LISTEN_PORT"`
	LogLevel     string `json:"PGRWL_LOG_LEVEL"`
	LogFormat    string `json:"PGRWL_LOG_FORMAT"`
	LogAddSource bool   `json:"PGRWL_LOG_ADD_SOURCE"`

	StorageType string `json:"PGRWL_STORAGE_TYPE"` // "local", "sftp", "s3"

	// Compression, Encryption
	CompressionAlgo string `json:"PGRWL_COMPRESSION_ALGO"` // gzip, zstd
	EncryptionAlgo  string `json:"PGRWL_ENCRYPTION_ALGO"`  // aes-256-gcm
	EncryptionPass  string `json:"PGRWL_ENCRYPTION_PASS"`

	// SFTP Storage config
	SFTPHost     string `json:"PGRWL_SFTP_HOST"`
	SFTPPort     int    `json:"PGRWL_SFTP_PORT"`
	SFTPUser     string `json:"PGRWL_SFTP_USER"`
	SFTPPass     string `json:"PGRWL_SFTP_PASS"`
	SFTPPkeyPath string `json:"PGRWL_SFTP_PKEY_PATH"`
	SFTPPkeyPass string `json:"PGRWL_SFTP_PKEY_PASS"`

	// S3 Storage config
	S3URL             string `json:"PGRWL_S3_URL"`
	S3AccessKeyID     string `json:"PGRWL_S3_ACCESS_KEY_ID"`
	S3SecretAccessKey string `json:"PGRWL_S3_SECRET_ACCESS_KEY"`
	S3Bucket          string `json:"PGRWL_S3_BUCKET"`
	S3Region          string `json:"PGRWL_S3_REGION"`
	S3UsePathStyle    bool   `json:"PGRWL_S3_USE_PATH_STYLE"`
	S3DisableSSL      bool   `json:"PGRWL_S3_DISABLE_SSL"`
}

func init() {
	// default values
	_ = os.Setenv("PGRWL_MODE", "receive")
	_ = os.Setenv("PGRWL_SLOT", "pgrwl_v5")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "7070")
	_ = os.Setenv("PGRWL_LOG_LEVEL", "info")
	_ = os.Setenv("PGRWL_LOG_FORMAT", "json")
	_ = os.Setenv("PGRWL_STORAGE_TYPE", "local")
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
		config, err = loadCfg(path)
		if err != nil {
			log.Fatal(err)
		}
	})
	return config
}

func loadCfg(path string) (*Config, error) {
	var c Config
	// if path is set, must read from file
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
	}
	// fill from env if JSON did not provide values
	mergeEnvIfUnset(&c)
	return &c, nil
}

// reset resets the singleton for test reuse
func reset() {
	once = sync.Once{}
	config = nil
}
