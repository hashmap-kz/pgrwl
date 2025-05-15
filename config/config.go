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
	Directory              string `json:"PGRWL_DIRECTORY"`
	ReceiveSlot            string `json:"PGRWL_RECEIVE_SLOT"`
	ReceiveNoLoop          bool   `json:"PGRWL_RECEIVE_NO_LOOP"`
	ReceiveListenPort      int    `json:"PGRWL_RECEIVE_LISTEN_PORT"`
	RestoreListenPort      int    `json:"PGRWL_RESTORE_LISTEN_PORT"`
	RestoreFetchAddr       string `json:"PGRWL_RESTORE_FETCH_ADDR"`
	LogLevel               string `json:"PGRWL_LOG_LEVEL"`
	LogFormat              string `json:"PGRWL_LOG_FORMAT"`
	LogAddSource           bool   `json:"PGRWL_LOG_ADD_SOURCE"`
	StorageType            string `json:"PGRWL_STORAGE_TYPE"`
	StorageCompressionAlgo string `json:"PGRWL_STORAGE_COMPRESSION_ALGO"`
	StorageEncryptionAlgo  string `json:"PGRWL_STORAGE_ENCRYPTION_ALGO"`
	StorageEncryptionPass  string `json:"PGRWL_STORAGE_ENCRYPTION_PASS"`
	SFTPHost               string `json:"PGRWL_SFTP_HOST"`
	SFTPPort               int    `json:"PGRWL_SFTP_PORT"`
	SFTPUser               string `json:"PGRWL_SFTP_USER"`
	SFTPPass               string `json:"PGRWL_SFTP_PASS"`
	SFTPPkeyPath           string `json:"PGRWL_SFTP_PKEY_PATH"`
	SFTPPkeyPass           string `json:"PGRWL_SFTP_PKEY_PASS"`
	S3URL                  string `json:"PGRWL_S3_URL"`
	S3AccessKeyID          string `json:"PGRWL_S3_ACCESS_KEY_ID"`
	S3SecretAccessKey      string `json:"PGRWL_S3_SECRET_ACCESS_KEY"`
	S3Bucket               string `json:"PGRWL_S3_BUCKET"`
	S3Region               string `json:"PGRWL_S3_REGION"`
	S3UsePathStyle         bool   `json:"PGRWL_S3_USE_PATH_STYLE"`
	S3DisableSSL           bool   `json:"PGRWL_S3_DISABLE_SSL"`
}

func init() {
	// default values
	_ = os.Setenv("PGRWL_RECEIVE_SLOT", "pgrwl_v5")
	_ = os.Setenv("PGRWL_RECEIVE_LISTEN_PORT", "7070")
	_ = os.Setenv("PGRWL_LOG_LEVEL", "info")
	_ = os.Setenv("PGRWL_LOG_FORMAT", "json")
	_ = os.Setenv("PGRWL_STORAGE_TYPE", "local")
	_ = os.Setenv("PGRWL_RESTORE_LISTEN_PORT", "7070")
	_ = os.Setenv("PGRWL_RESTORE_FETCH_ADDR", "http://127.0.0.1:7070")
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
