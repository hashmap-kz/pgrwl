package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"
)

const (
	ModeReceive = "receive"
	ModeServe   = "serve"

	StorageNameS3   = "s3"
	StorageNameSFTP = "sftp"

	RepoEncryptorAes256Gcm string = "aes-256-gcm"
	RepoCompressorGzip     string = "gzip"
	RepoCompressorZstd     string = "zstd"
)

var (
	once   sync.Once
	config *Config
)

type Config struct {
	Main      MainConfig      `json:"main,omitempty"`
	Receiver  ReceiveConfig   `json:"receiver,omitempty"`
	Uploader  UploadConfig    `json:"uploader,omitempty"`
	Retention RetentionConfig `json:"retention,omitempty"`
	Log       LogConfig       `json:"log,omitempty"`
	Storage   StorageConfig   `json:"storage,omitempty"`
}

// ---- Main Section ----

type MainConfig struct {
	ListenPort int    `json:"listen_port,omitempty"`
	Directory  string `json:"directory,omitempty"`
}

// ---- Receiver ----

type ReceiveConfig struct {
	Slot   string `json:"slot,omitempty"`
	NoLoop bool   `json:"no_loop,omitempty"`
}

// ---- Uploader ----

type UploadConfig struct {
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval   string `json:"sync_interval"`
	MaxConcurrency int    `json:"max_concurrency"`
}

// ---- Retention ----

type RetentionConfig struct {
	Enable bool `json:"enable,omitempty"`
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval string `json:"sync_interval"`
	KeepPeriod   string `json:"keep_period,omitempty"`
}

// ---- Log Section ----

type LogConfig struct {
	Level     string `json:"level,omitempty"`      // e.g. "info"
	Format    string `json:"format,omitempty"`     // e.g. "text" or "json"
	AddSource bool   `json:"add_source,omitempty"` // whether to include source info
}

// ---- Storage Section ----

type StorageConfig struct {
	Name        string            `json:"name,omitempty"` // e.g. "s3", "sftp"
	Compression CompressionConfig `json:"compression,omitempty"`
	Encryption  EncryptionConfig  `json:"encryption,omitempty"`
	SFTP        SFTPConfig        `json:"sftp,omitempty"`
	S3          S3Config          `json:"s3,omitempty"`
}

type CompressionConfig struct {
	Algo string `json:"algo,omitempty"` // e.g. "gzip"
}

type EncryptionConfig struct {
	Algo string `json:"algo,omitempty"` // e.g. "aesgcm"
	Pass string `json:"pass,omitempty"` // can reference env var
}

type SFTPConfig struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Pass     string `json:"pass,omitempty"`
	PKeyPath string `json:"pkey_path,omitempty"`
	PKeyPass string `json:"pkey_pass,omitempty"`
}

type S3Config struct {
	URL             string `json:"url,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	Region          string `json:"region,omitempty"`
	UsePathStyle    bool   `json:"use_path_style,omitempty"`
	DisableSSL      bool   `json:"disable_ssl,omitempty"`
}

func (c *Config) HasExternalStorageConfigured() bool {
	switch c.Storage.Name {
	case StorageNameS3, StorageNameSFTP:
		return true
	}
	return false
}

func (c *Config) String() string {
	// Step 1: Make a shallow copy
	cp := *c

	// Step 2: Redact sensitive fields (distinct between empty and filled)
	const redacted = "[REDACTED]"
	if cp.Storage.Encryption.Pass != "" {
		cp.Storage.Encryption.Pass = redacted
	}
	if cp.Storage.SFTP.Pass != "" {
		cp.Storage.SFTP.Pass = redacted
	}
	if cp.Storage.SFTP.PKeyPass != "" {
		cp.Storage.SFTP.PKeyPass = redacted
	}
	if cp.Storage.S3.SecretAccessKey != "" {
		cp.Storage.S3.SecretAccessKey = redacted
	}

	// Step 3: Marshal the copy
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func expandEnvsWithPrefix(input, prefix string) string {
	return os.Expand(input, func(key string) string {
		if strings.HasPrefix(key, prefix) {
			return os.Getenv(key)
		}
		// Leave unexpanded
		return "${" + key + "}"
	})
}

func expand(d []byte) []byte {
	return []byte(expandEnvsWithPrefix(string(d), "PGRWL_"))
}

func Cfg() *Config {
	if config == nil {
		log.Fatal("config was not loaded in main")
	}
	return config
}

func MustLoad(path string) *Config {
	once.Do(func() {
		config = mustLoadCfg(path)
	})
	return config
}

func mustLoadCfg(path string) *Config {
	var cfg Config
	configData, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		if err := json.Unmarshal(expand(configData), &cfg); err != nil {
			log.Fatal(err)
		}
	case ".yml", ".yaml":
		if err := yaml.Unmarshal(expand(configData), &cfg); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unexpected config-file extension: %s", ext)
	}
	return &cfg
}
