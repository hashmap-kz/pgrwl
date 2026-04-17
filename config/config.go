package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sethvargo/go-envconfig"

	"sigs.k8s.io/yaml"
)

// Constants for application modes.
const (
	// ModeReceive is the only long-running daemon mode.
	// It receives WAL from PostgreSQL, serves WAL files for restore_command,
	// and exposes receiver lifecycle control via the HTTP API.
	ModeReceive = "receive"

	// ModeBackup runs the scheduled base-backup daemon.
	ModeBackup = "backup"

	// ModeBackupCMD is used by the `pgrwl backup` CLI command.
	ModeBackupCMD = "backup-cmd"

	// ModeRestoreCMD is used by the `pgrwl restore` CLI command.
	ModeRestoreCMD = "restore"

	// StorageNameS3 is the identifier for the S3 storage backend.
	StorageNameS3 = "s3"

	// StorageNameSFTP is the identifier for the SFTP storage backend.
	StorageNameSFTP = "sftp"

	// StorageNameLocalFS is the identifier for the local filesystem storage.
	StorageNameLocalFS = "local"

	// LocalFSStorageSubpath is the subdirectory used by the uploader worker
	// when storage.name is "local".
	LocalFSStorageSubpath = "wal-archive"

	// BaseBackupSubpath is the subdirectory used for base backups when
	// storage.name is "local".
	BaseBackupSubpath = "backups"

	// RepoEncryptorAes256Gcm is the AES-256-GCM encryption algorithm identifier.
	RepoEncryptorAes256Gcm = "aes-256-gcm"

	// RepoCompressorGzip is the Gzip compression algorithm identifier.
	RepoCompressorGzip = "gzip"

	// RepoCompressorZstd is the Zstandard compression algorithm identifier.
	RepoCompressorZstd = "zstd"

	BackupRetentionTypeTime  = "time"
	BackupRetentionTypeCount = "count"
)

var (
	// once ensures config is initialized only once.
	once sync.Once

	// config holds the global application configuration.
	config *Config

	modes = []string{
		ModeBackupCMD,
		ModeRestoreCMD,
		ModeBackup,
		ModeReceive,
	}
)

// Config is the root configuration for the WAL receiver application.
// Supports `${PGRWL_*}` environment variable placeholders for sensitive values.
type Config struct {
	Main      MainConfig    `json:"main,omitzero"`      // Main application settings.
	Receiver  ReceiveConfig `json:"receiver,omitzero"`  // WAL receiver configuration.
	Metrics   MetricsConfig `json:"metrics,omitzero"`   // Prometheus metrics configuration.
	Log       LogConfig     `json:"log,omitzero"`       // Logging configuration.
	Storage   StorageConfig `json:"storage,omitzero"`   // Storage backend configuration.
	DevConfig DevConfig     `json:"devconfig,omitzero"` // Various dev options.
	Backup    BackupConfig  `json:"backup,omitzero"`    // Streaming basebackup options.
}

// MainConfig holds top-level application settings.
type MainConfig struct {
	// ListenPort is the TCP port for the management HTTP server.
	ListenPort int `json:"listen_port,omitzero" env:"PGRWL_MAIN_LISTEN_PORT"`

	// Directory is the base directory where WAL files and metadata are stored.
	Directory string `json:"directory,omitzero" env:"PGRWL_MAIN_DIRECTORY"`
}

// DevConfig configures development-only features like profiling and debug endpoints.
type DevConfig struct {
	Pprof DevConfigPprof `json:"pprof,omitzero"`
}

// DevConfigPprof configures pprof.
type DevConfigPprof struct {
	Enable bool `json:"enable,omitzero" env:"PGRWL_DEVCONFIG_PPROF_ENABLE"`
}

// BackupConfig configures streaming basebackup properties.
type BackupConfig struct {
	Cron         string                   `json:"cron" env:"PGRWL_BACKUP_CRON"`
	Retention    BackupRetentionConfig    `json:"retention,omitzero"`
	WalRetention BackupWalRetentionConfig `json:"walretention,omitzero"`
}

// BackupRetentionConfig configures retention for basebackups.
type BackupRetentionConfig struct {
	// Enable determines whether retention logic is active.
	Enable bool `json:"enable,omitzero" env:"PGRWL_BACKUP_RETENTION_ENABLE"`

	Type  string `json:"type,omitzero" env:"PGRWL_BACKUP_RETENTION_TYPE"`
	Value string `json:"value,omitzero" env:"PGRWL_BACKUP_RETENTION_VALUE"`

	KeepDurationParsed time.Duration `json:"-"`
	KeepCountParsed    int64         `json:"-"`

	KeepLast *int `json:"keep_last,omitzero" env:"PGRWL_BACKUP_RETENTION_KEEP_LAST"`
}

// BackupWalRetentionConfig configures related settings for the WAL archive.
type BackupWalRetentionConfig struct {
	Enable       bool   `json:"enable,omitzero" env:"PGRWL_BACKUP_WALRETENTION_ENABLE"`
	ReceiverAddr string `json:"receiver_addr,omitzero" env:"PGRWL_BACKUP_WALRETENTION_RECEIVER_ADDR"`
}

// ReceiveConfig configures the WAL receiving logic.
type ReceiveConfig struct {
	// Slot is the replication slot name used to stream WAL from PostgreSQL.
	Slot string `json:"slot,omitzero" env:"PGRWL_RECEIVER_SLOT"`

	// NoLoop disables automatic reconnection loops on connection loss.
	NoLoop bool `json:"no_loop,omitzero" env:"PGRWL_RECEIVER_NO_LOOP"`

	// Uploader worker configuration.
	Uploader UploadConfig `json:"uploader,omitzero"`

	// Retention policy configuration.
	Retention RetentionConfig `json:"retention,omitzero"`
}

// UploadConfig configures the uploader worker.
type UploadConfig struct {
	// SyncInterval is the interval between upload checks (e.g., "10s", "5m").
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval       string        `json:"sync_interval" env:"PGRWL_RECEIVER_UPLOADER_SYNC_INTERVAL"`
	SyncIntervalParsed time.Duration `json:"-"`

	// MaxConcurrency is the maximum number of concurrent upload tasks.
	MaxConcurrency int `json:"max_concurrency" env:"PGRWL_RECEIVER_UPLOADER_MAX_CONCURRENCY"`
}

// RetentionConfig configures the WAL file retention worker.
type RetentionConfig struct {
	// Enable determines whether retention logic is active.
	Enable bool `json:"enable,omitzero" env:"PGRWL_RECEIVER_RETENTION_ENABLE"`

	// SyncInterval is the interval between retention scans (e.g., "12h").
	// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
	SyncInterval       string        `json:"sync_interval" env:"PGRWL_RECEIVER_RETENTION_SYNC_INTERVAL"`
	SyncIntervalParsed time.Duration `json:"-"`

	// KeepPeriod defines how long to keep old WAL files (e.g., "72h").
	KeepPeriod       string        `json:"keep_period,omitzero" env:"PGRWL_RECEIVER_RETENTION_KEEP_PERIOD"`
	KeepPeriodParsed time.Duration `json:"-"`
}

// MetricsConfig enables or disables Prometheus metrics exposure.
type MetricsConfig struct {
	// Enable turns on Prometheus metrics HTTP endpoint.
	Enable bool `json:"enable,omitzero" env:"PGRWL_METRICS_ENABLE"`
}

// LogConfig defines application logging options.
type LogConfig struct {
	// Level sets the log level (e.g., "trace", "debug", "info", "warn", "error").
	Level string `json:"level,omitzero" env:"PGRWL_LOG_LEVEL"`

	// Format sets the log format ("text" or "json").
	Format string `json:"format,omitzero" env:"PGRWL_LOG_FORMAT"`

	// AddSource includes source file and line in log entries.
	AddSource bool `json:"add_source,omitzero" env:"PGRWL_LOG_ADD_SOURCE"`
}

// StorageConfig defines which storage backend to use and its options.
type StorageConfig struct {
	// Name specifies the storage backend to use ("s3", "sftp", etc.).
	Name string `json:"name,omitzero" env:"PGRWL_STORAGE_NAME"`

	// Compression defines compression settings for stored WAL files.
	Compression CompressionConfig `json:"compression,omitzero"`

	// Encryption defines encryption settings for stored WAL files.
	Encryption EncryptionConfig `json:"encryption,omitzero"`

	// SFTP holds configuration specific to the SFTP backend.
	SFTP SFTPConfig `json:"sftp,omitzero"`

	// S3 holds configuration specific to the S3 backend.
	S3 S3Config `json:"s3,omitzero"`
}

// CompressionConfig defines the compression algorithm to use.
type CompressionConfig struct {
	// Algo is the compression algorithm ("gzip", "zstd", etc.).
	Algo string `json:"algo,omitzero" env:"PGRWL_STORAGE_COMPRESSION_ALGO"`
}

// EncryptionConfig defines the encryption algorithm and credentials.
type EncryptionConfig struct {
	// Algo is the encryption algorithm identifier ("aes-256-gcm", etc.).
	Algo string `json:"algo,omitzero" env:"PGRWL_STORAGE_ENCRYPTION_ALGO"`

	// Pass is the encryption passphrase.
	Pass string `json:"pass,omitzero" env:"PGRWL_STORAGE_ENCRYPTION_PASS"`
}

// SFTPConfig defines parameters for connecting to an SFTP server.
type SFTPConfig struct {
	Host     string `json:"host,omitzero" env:"PGRWL_STORAGE_SFTP_HOST"`
	Port     int    `json:"port,omitzero" env:"PGRWL_STORAGE_SFTP_PORT"`
	User     string `json:"user,omitzero" env:"PGRWL_STORAGE_SFTP_USER"`
	Pass     string `json:"pass,omitzero" env:"PGRWL_STORAGE_SFTP_PASS"`
	PKeyPath string `json:"pkey_path,omitzero" env:"PGRWL_STORAGE_SFTP_PKEY_PATH"`
	PKeyPass string `json:"pkey_pass,omitzero" env:"PGRWL_STORAGE_SFTP_PKEY_PASS"`
	BaseDir  string `json:"base_dir,omitzero" env:"PGRWL_STORAGE_SFTP_BASE_DIR"`
}

// S3Config defines configuration for S3-compatible object storage.
type S3Config struct {
	URL             string `json:"url,omitzero" env:"PGRWL_STORAGE_S3_URL"`
	AccessKeyID     string `json:"access_key_id,omitzero" env:"PGRWL_STORAGE_S3_ACCESS_KEY_ID"`
	SecretAccessKey string `json:"secret_access_key,omitzero" env:"PGRWL_STORAGE_S3_SECRET_ACCESS_KEY"`
	Bucket          string `json:"bucket,omitzero" env:"PGRWL_STORAGE_S3_BUCKET"`
	Region          string `json:"region,omitzero" env:"PGRWL_STORAGE_S3_REGION"`
	UsePathStyle    bool   `json:"use_path_style,omitzero" env:"PGRWL_STORAGE_S3_USE_PATH_STYLE"`
	DisableSSL      bool   `json:"disable_ssl,omitzero" env:"PGRWL_STORAGE_S3_DISABLE_SSL"`
}

// String returns a pretty-printed structure where sensitive fields are hidden.
func (c *Config) String() string {
	cp := *c
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

func MustEnvconfig(mode string) *Config {
	once.Do(func() {
		config = new(Config)
		if err := envconfig.Process(context.TODO(), config); err != nil {
			log.Fatalf("Invalid config: %v", err)
		}
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

func checkMode(mode string, errs []string) []string {
	for _, m := range modes {
		if strings.EqualFold(m, mode) {
			return errs
		}
	}
	errs = append(errs, fmt.Sprintf("invalid mode: %q (must be one of: %s)", mode, strings.Join(modes, "|")))
	return errs
}

func checkMainConfig(c *Config, errs []string) []string {
	if c.Main.ListenPort == 0 {
		errs = append(errs, "main.listen_port is required")
	}
	if strings.TrimSpace(c.Main.Directory) == "" {
		errs = append(errs, "main.directory is required")
	}
	return errs
}

func checkReceiverConfig(c *Config, mode string, errs []string) []string {
	if mode != ModeReceive {
		return errs
	}
	if strings.TrimSpace(c.Receiver.Slot) == "" {
		errs = append(errs, "receiver.slot is required in receive mode")
	}
	// uploader conf is required when external storage is used, or when local
	// storage is used with compression/encryption configured.
	if c.IsExternalStor() || c.Storage.Compression.Algo != "" || c.Storage.Encryption.Algo != "" {
		if duration, err := time.ParseDuration(c.Receiver.Uploader.SyncInterval); err != nil {
			errs = append(errs, fmt.Sprintf("receiver.uploader.sync_interval cannot parse: %s, %v",
				c.Receiver.Uploader.SyncInterval, err))
		} else {
			c.Receiver.Uploader.SyncIntervalParsed = duration
		}
		if c.Receiver.Uploader.MaxConcurrency <= 0 {
			errs = append(errs, "receiver.uploader.max_concurrency must be > 0 if uploader is configured")
		}
	}
	if c.Receiver.Retention.Enable {
		if duration, err := time.ParseDuration(c.Receiver.Retention.SyncInterval); err != nil {
			errs = append(errs, fmt.Sprintf("receiver.retention.sync_interval cannot parse: %s, %v",
				c.Receiver.Retention.SyncInterval, err))
		} else {
			c.Receiver.Retention.SyncIntervalParsed = duration
		}
		if duration, err := time.ParseDuration(c.Receiver.Retention.KeepPeriod); err != nil {
			errs = append(errs, fmt.Sprintf("receiver.retention.keep_period cannot parse: %s, %v",
				c.Receiver.Retention.KeepPeriod, err))
		} else {
			c.Receiver.Retention.KeepPeriodParsed = duration
		}
	}
	return errs
}

func checkStorageModifiersConfig(c *Config, errs []string) []string {
	if c.Storage.Compression.Algo != "" {
		if c.Storage.Compression.Algo != RepoCompressorGzip && c.Storage.Compression.Algo != RepoCompressorZstd {
			errs = append(errs, fmt.Sprintf("unsupported compression algo: %s", c.Storage.Compression.Algo))
		}
	}
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
	if mode != ModeBackup {
		return errs
	}
	if c.Backup.Cron == "" {
		errs = append(errs, "backup.cron is required in backup mode")
	}
	if c.Backup.Retention.Enable {
		switch c.Backup.Retention.Type {
		case BackupRetentionTypeTime:
			if duration, err := time.ParseDuration(c.Backup.Retention.Value); err != nil {
				errs = append(errs, fmt.Sprintf("backup.retention.value cannot parse duration: %s, %v",
					c.Backup.Retention.Value, err))
			} else {
				c.Backup.Retention.KeepDurationParsed = duration
			}
		case BackupRetentionTypeCount:
			if n, err := strconv.ParseInt(c.Backup.Retention.Value, 10, 32); err != nil || n <= 0 {
				errs = append(errs, fmt.Sprintf("backup.retention.value cannot parse count: %s, %v",
					c.Backup.Retention.Value, err))
			} else {
				c.Backup.Retention.KeepCountParsed = n
			}
		default:
			errs = append(errs, fmt.Sprintf("backup.retention.type: must be one of: time/count (got: %q)",
				c.Backup.Retention.Type))
		}
	}
	return errs
}

func checkLogConfig(c *Config, errs []string) []string {
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
