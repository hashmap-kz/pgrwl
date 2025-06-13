package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/yaml"
)

// Constants for application modes.
const (
	// ModeReceive represents the WAL receiving mode.
	ModeReceive = "receive"

	// ModeServe represents the HTTP API serving mode.
	ModeServe = "serve"

	// ModeBackup used in pgrwl streaming basebackup mode.
	ModeBackup = "backup"

	// ModeBackupCMD used in pgrwl backup CLI command.
	ModeBackupCMD = "backup-cmd"

	// ModeRestoreCMD used in pgrwl restore CLI command.
	ModeRestoreCMD = "restore"

	// StorageNameS3 is the identifier for the S3 storage backend.
	StorageNameS3 = "s3"

	// StorageNameSFTP is the identifier for the SFTP storage backend.
	StorageNameSFTP = "sftp"

	// StorageNameLocalFS is the identifier for the local storage.
	StorageNameLocalFS = "local"

	// LocalFSStorageSubpath when storage name is 'local', uploader worker uses this as a storage.
	LocalFSStorageSubpath = "wal-archive"

	// BaseBackupSubpath when storage name is 'local', put basebackups to this directory.
	BaseBackupSubpath = "backups"

	// RepoEncryptorAes256Gcm is the AES-256-GCM encryption algorithm identifier.
	RepoEncryptorAes256Gcm = "aes-256-gcm"

	// RepoCompressorGzip is the Gzip compression algorithm identifier.
	RepoCompressorGzip = "gzip"

	// RepoCompressorZstd is the Zstandard compression algorithm identifier.
	RepoCompressorZstd = "zstd"
)

var (
	// once ensures config is initialized only once.
	once sync.Once

	// config holds the global application configuration.
	config *Config

	modes = []string{
		// CMD
		ModeBackupCMD,
		ModeRestoreCMD,
		// serving
		ModeBackup,
		ModeReceive,
		ModeServe,
	}
)

// Config is the root configuration for the WAL receiver application.
// Supports `${PGRWL_*}` environment variable placeholders for sensitive values.
type Config struct {
	Main      MainConfig    `json:"main,omitempty"`       // Main application settings.
	Receiver  ReceiveConfig `json:"receiver,omitempty"`   // WAL receiver configuration.
	Metrics   MetricsConfig `json:"metrics,omitempty"`    // Prometheus metrics configuration.
	Log       LogConfig     `json:"log,omitempty"`        // Logging configuration.
	Storage   StorageConfig `json:"storage,omitempty"`    // Storage backend configuration.
	DevConfig DevConfig     `json:"dev_config,omitempty"` // Various dev options.
	Backup    BackupConfig  `json:"backup,omitempty"`     // Streaming basebackup options.
}

// MainConfig holds top-level application settings.
type MainConfig struct {
	// ListenPort is the TCP port for the management HTTP server.
	ListenPort int `json:"listen_port,omitempty"`

	// Directory is the base directory where WAL files and metadata are stored.
	Directory string `json:"directory,omitempty"`
}

// DevConfig configures development-only features like profiling and debug endpoints.
type DevConfig struct {
	Pprof DevConfigPprof `json:"pprof,omitempty"`
}

// DevConfigPprof configures pprof.
type DevConfigPprof struct {
	Enable bool `json:"enable,omitempty"`
}

// BackupConfig configures streaming basebackup properties.
type BackupConfig struct {
	Cron      string                `json:"cron"`
	Retention BackupRetentionConfig `json:"retention,omitempty"`
}

// BackupRetentionConfig configures retention for basebackups.
type BackupRetentionConfig struct {
	// Enable determines whether retention logic is active.
	Enable bool `json:"enable,omitempty"`

	// KeepPeriod defines how long to keep old WAL files (e.g., "72h").
	KeepPeriod       string        `json:"keep_period,omitempty"`
	KeepPeriodParsed time.Duration `json:"-"`
}

// ReceiveConfig configures the WAL receiving logic.
type ReceiveConfig struct {
	// Slot is the replication slot name used to stream WAL from PostgreSQL.
	Slot string `json:"slot,omitempty"`

	// NoLoop disables automatic reconnection loops on connection loss.
	NoLoop bool `json:"no_loop,omitempty"`

	// Uploader worker configuration.
	Uploader UploadConfig `json:"uploader,omitempty"`

	// Retention policy configuration.
	Retention RetentionConfig `json:"retention,omitempty"`
}

// UploadConfig configures the uploader worker.
type UploadConfig struct {
	// SyncInterval is the interval between upload checks (e.g., "10s", "5m").
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval       string        `json:"sync_interval"`
	SyncIntervalParsed time.Duration `json:"-"`

	// MaxConcurrency is the maximum number of concurrent upload tasks.
	MaxConcurrency int `json:"max_concurrency"`
}

// RetentionConfig configures the WAL file retention worker.
type RetentionConfig struct {
	// Enable determines whether retention logic is active.
	Enable bool `json:"enable,omitempty"`

	// SyncInterval is the interval between retention scans (e.g., "12h").
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval       string        `json:"sync_interval"`
	SyncIntervalParsed time.Duration `json:"-"`

	// KeepPeriod defines how long to keep old WAL files (e.g., "72h").
	KeepPeriod       string        `json:"keep_period,omitempty"`
	KeepPeriodParsed time.Duration `json:"-"`
}

// MetricsConfig enables or disables Prometheus metrics exposure.
type MetricsConfig struct {
	// Enable turns on Prometheus metrics HTTP endpoint.
	Enable bool `json:"enable,omitempty"`
}

// LogConfig defines application logging options.
type LogConfig struct {
	// Level sets the log level (e.g., "trace", "debug", "info", "warn", "error").
	Level string `json:"level,omitempty"`

	// Format sets the log format ("text" or "json").
	Format string `json:"format,omitempty"`

	// AddSource includes source file and line in log entries.
	AddSource bool `json:"add_source,omitempty"`
}

// StorageConfig defines which storage backend to use and its options.
type StorageConfig struct {
	// Name specifies the storage backend to use ("s3", "sftp", etc.).
	Name string `json:"name,omitempty"`

	// Compression defines compression settings for stored WAL files.
	Compression CompressionConfig `json:"compression,omitempty"`

	// Encryption defines encryption settings for stored WAL files.
	Encryption EncryptionConfig `json:"encryption,omitempty"`

	// SFTP holds configuration specific to the SFTP backend.
	SFTP SFTPConfig `json:"sftp,omitempty"`

	// S3 holds configuration specific to the S3 backend.
	S3 S3Config `json:"s3,omitempty"`
}

// CompressionConfig defines the compression algorithm to use.
type CompressionConfig struct {
	// Algo is the compression algorithm ("gzip", "zstd", etc.).
	Algo string `json:"algo,omitempty"`
}

// EncryptionConfig defines the encryption algorithm and credentials.
type EncryptionConfig struct {
	// Algo is the encryption algorithm identifier ("aes-256-gcm", etc.).
	Algo string `json:"algo,omitempty"`

	// Pass is the encryption passphrase.
	Pass string `json:"pass,omitempty"`
}

// SFTPConfig defines parameters for connecting to an SFTP server.
type SFTPConfig struct {
	// Host is the SFTP server hostname or IP.
	Host string `json:"host,omitempty"`

	// Port is the TCP port for the SFTP server.
	Port int `json:"port,omitempty"`

	// User is the username for SFTP authentication.
	User string `json:"user,omitempty"`

	// Pass is the password for SFTP authentication (if not using a key).
	Pass string `json:"pass,omitempty"`

	// PKeyPath is the file path to the private key for key-based authentication.
	PKeyPath string `json:"pkey_path,omitempty"`

	// PKeyPass is the passphrase for the private key, if encrypted.
	PKeyPass string `json:"pkey_pass,omitempty"`

	// Base directory with sufficient user permissions
	BaseDir string `json:"base_dir,omitempty"`
}

// S3Config defines configuration for S3-compatible object storage.
type S3Config struct {
	// URL is the S3-compatible endpoint URL (e.g., "https://s3.amazonaws.com").
	URL string `json:"url,omitempty"`

	// AccessKeyID is the S3 access key ID.
	AccessKeyID string `json:"access_key_id,omitempty"`

	// SecretAccessKey is the S3 secret access key.
	SecretAccessKey string `json:"secret_access_key,omitempty"`

	// Bucket is the name of the S3 bucket to store WAL files.
	Bucket string `json:"bucket,omitempty"`

	// Region is the AWS region (for Amazon S3).
	Region string `json:"region,omitempty"`

	// UsePathStyle forces path-style requests instead of virtual-hosted style.
	UsePathStyle bool `json:"use_path_style,omitempty"`

	// DisableSSL disables HTTPS for connections to the S3 endpoint.
	DisableSSL bool `json:"disable_ssl,omitempty"`
}

// String returns a pretty-printed structure where sensitive fields are hidden.
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

func MustLoad(path, mode string) *Config {
	once.Do(func() {
		config = mustLoadCfg(path)
		if err := validate(config, mode); err != nil {
			log.Fatalf("Invalid config: %v", err)
		}
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

// validate checks that all required fields in the config are set appropriately.
func validate(c *Config, mode string) error {
	var errs []string

	if strings.TrimSpace(mode) == "" {
		return fmt.Errorf("config.validate: mode is required")
	}

	errs = checkMode(mode, errs)
	errs = checkMainConfig(c, errs)
	errs = checkReceiverConfig(c, mode, errs)
	errs = checkBackupConfig(c, mode, errs)
	errs = checkLogConfig(c, errs)
	errs = checkStorageConfig(c, errs)
	errs = checkStorageModifiersConfig(c, errs)

	if len(errs) > 0 {
		return errors.New("invalid config:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}

// checks

func checkMode(mode string, errs []string) []string {
	found := false
	for _, m := range modes {
		if strings.EqualFold(m, mode) {
			found = true
			break
		}
	}
	if !found {
		errs = append(errs, fmt.Sprintf("invalid mode: %q (must be %q)", mode, strings.Join(modes, "|")))
	}
	return errs
}

func checkMainConfig(c *Config, errs []string) []string {
	// Validate main section
	if c.Main.ListenPort == 0 {
		errs = append(errs, "main.listen_port is required")
	}
	if strings.TrimSpace(c.Main.Directory) == "" {
		errs = append(errs, "main.directory is required")
	}
	return errs
}

func checkReceiverConfig(c *Config, mode string, errs []string) []string {
	// Validate receiver (only in receive mode)
	if mode == ModeReceive {
		if strings.TrimSpace(c.Receiver.Slot) == "" {
			errs = append(errs, "receiver.slot is required in receive mode")
		}
		// uploader conf is required:
		// * when external storage is used
		// * when local storage used with compression || encryption configured
		if c.IsExternalStor() || c.Storage.Compression.Algo != "" || c.Storage.Encryption.Algo != "" {
			// uploader
			syncIntervalUploader := c.Receiver.Uploader.SyncInterval
			if duration, err := time.ParseDuration(syncIntervalUploader); err != nil {
				errs = append(errs, fmt.Sprintf("receiver.uploader.sync_interval cannot parse: %s, %v", syncIntervalUploader, err))
			} else {
				c.Receiver.Uploader.SyncIntervalParsed = duration
			}
			if c.Receiver.Uploader.MaxConcurrency <= 0 {
				errs = append(errs, "receiver.uploader.max_concurrency must be > 0 if uploader is configured")
			}
		}
		// retention
		if c.Receiver.Retention.Enable {
			syncIntervalRetention := c.Receiver.Retention.SyncInterval
			if duration, err := time.ParseDuration(syncIntervalRetention); err != nil {
				errs = append(errs, fmt.Sprintf("receiver.retention.sync_interval cannot parse: %s, %v", syncIntervalRetention, err))
			} else {
				c.Receiver.Retention.SyncIntervalParsed = duration
			}
			keepPeriodRetention := c.Receiver.Retention.KeepPeriod
			if duration, err := time.ParseDuration(keepPeriodRetention); err != nil {
				errs = append(errs, fmt.Sprintf("receiver.retention.keep_period cannot parse: %s, %v", keepPeriodRetention, err))
			} else {
				c.Receiver.Retention.KeepPeriodParsed = duration
			}
		}
	}
	return errs
}

func checkStorageModifiersConfig(c *Config, errs []string) []string {
	// Validate optional compression
	if c.Storage.Compression.Algo != "" {
		if c.Storage.Compression.Algo != RepoCompressorGzip && c.Storage.Compression.Algo != RepoCompressorZstd {
			errs = append(errs, fmt.Sprintf("unsupported compression algo: %s", c.Storage.Compression.Algo))
		}
	}

	// Validate optional encryption
	if c.Storage.Encryption.Algo != "" {
		if c.Storage.Encryption.Algo != RepoEncryptorAes256Gcm {
			errs = append(errs, fmt.Sprintf("unsupported encryption algo: %s", c.Storage.Encryption.Algo))
		}
		if c.Storage.Encryption.Pass == "" {
			errs = append(errs, "storage.encryption.pass is required if encryption is enabled")
		}
	}
	return errs
}

func checkStorageConfig(c *Config, errs []string) []string {
	// Validate storage
	switch c.Storage.Name {
	case "", StorageNameLocalFS:
		// ok, storage is optional
	case StorageNameS3:
		s3 := c.Storage.S3
		if s3.URL == "" {
			errs = append(errs, "storage.s3.url is required for s3 storage")
		}
		if s3.AccessKeyID == "" {
			errs = append(errs, "storage.s3.access_key_id is required for s3 storage")
		}
		if s3.SecretAccessKey == "" {
			errs = append(errs, "storage.s3.secret_access_key is required for s3 storage")
		}
		if s3.Bucket == "" {
			errs = append(errs, "storage.s3.bucket is required for s3 storage")
		}
		if s3.Region == "" {
			errs = append(errs, "storage.s3.region is required for s3 storage")
		}
	case StorageNameSFTP:
		sftp := c.Storage.SFTP
		if sftp.Host == "" {
			errs = append(errs, "storage.sftp.host is required for sftp storage")
		}
		if sftp.Port == 0 {
			errs = append(errs, "storage.sftp.port is required for sftp storage")
		}
		if sftp.User == "" {
			errs = append(errs, "storage.sftp.user is required for sftp storage")
		}
		if sftp.Pass == "" && sftp.PKeyPath == "" {
			errs = append(errs, "either storage.sftp.pass or storage.sftp.pkey_path must be provided for sftp storage")
		}
		if sftp.BaseDir == "" {
			errs = append(errs, "storage.sftp.base_dir is required for sftp storage")
		}
	default:
		errs = append(errs, fmt.Sprintf("unknown storage.name: %q (must be %q or %q)", c.Storage.Name, StorageNameS3, StorageNameSFTP))
	}
	return errs
}

func checkBackupConfig(c *Config, mode string, errs []string) []string {
	// Backup
	if mode == ModeBackup {
		if c.Backup.Cron == "" {
			errs = append(errs, "backup.cron is required in backup mode")
		}
		if c.Backup.Retention.Enable {
			basebackupKeepPeriodParsed, err := time.ParseDuration(c.Backup.Retention.KeepPeriod)
			if err != nil {
				errs = append(errs, fmt.Sprintf(
					"backup.retention.keep_period cannot parse: %s, %v",
					c.Backup.Retention.KeepPeriod, err),
				)
			} else {
				c.Backup.Retention.KeepPeriodParsed = basebackupKeepPeriodParsed
			}
		}
	}
	return errs
}

func checkLogConfig(c *Config, errs []string) []string {
	// Validate log (optional)
	if c.Log.Level != "" {
		validLevels := map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[strings.ToLower(c.Log.Level)] {
			errs = append(errs, fmt.Sprintf("log.level must be one of: trace/debug/info/warn/error (got: %s)", c.Log.Level))
		}
	}
	if c.Log.Format != "" {
		if c.Log.Format != "text" && c.Log.Format != "json" {
			errs = append(errs, fmt.Sprintf("log.format must be 'text' or 'json' (got: %s)", c.Log.Format))
		}
	}
	return errs
}

// helpers

func (c *Config) IsExternalStor() bool {
	return !c.IsLocalStor()
}

func (c *Config) IsLocalStor() bool {
	return strings.EqualFold(c.Storage.Name, StorageNameLocalFS) ||
		strings.TrimSpace(c.Storage.Name) == ""
}
