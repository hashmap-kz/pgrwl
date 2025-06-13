package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExpandEnvsWithPrefix(t *testing.T) {
	// Set test environment variables
	t.Setenv("PGRWL_FOO", "foo-val")
	t.Setenv("PGRWL_BAR", "bar-val")
	t.Setenv("OTHER_BAZ", "should-not-expand")

	tests := []struct {
		name     string
		input    string
		prefix   string
		expected string
	}{
		{
			name:     "expand single matching var",
			input:    "value=${PGRWL_FOO}",
			prefix:   "PGRWL_",
			expected: "value=foo-val",
		},
		{
			name:     "expand multiple matching vars",
			input:    "one=${PGRWL_FOO}, two=${PGRWL_BAR}",
			prefix:   "PGRWL_",
			expected: "one=foo-val, two=bar-val",
		},
		{
			name:     "ignore unmatched var (wrong prefix)",
			input:    "value=${OTHER_BAZ}",
			prefix:   "PGRWL_",
			expected: "value=${OTHER_BAZ}",
		},
		{
			name:     "mixed matched and unmatched vars",
			input:    "a=${PGRWL_FOO}, b=${OTHER_BAZ}",
			prefix:   "PGRWL_",
			expected: "a=foo-val, b=${OTHER_BAZ}",
		},
		{
			name:     "undefined env var with correct prefix",
			input:    "value=${PGRWL_UNKNOWN}",
			prefix:   "PGRWL_",
			expected: "value=",
		},
		{
			name:     "empty input string",
			input:    "",
			prefix:   "PGRWL_",
			expected: "",
		},
		{
			name:     "no variable placeholders",
			input:    "static string",
			prefix:   "PGRWL_",
			expected: "static string",
		},
		{
			name:     "empty prefix allows all expansions",
			input:    "x=${PGRWL_FOO}, y=${OTHER_BAZ}",
			prefix:   "",
			expected: "x=foo-val, y=should-not-expand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := expandEnvsWithPrefix(tt.input, tt.prefix)
			assert.Equal(t, tt.expected, out)
		})
	}
}

func TestValidate_Config(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		mode        string
		expectError bool
		wantMsgs    []string // optional substring checks
	}{
		{
			name: "valid receive config with s3",
			mode: ModeReceive,
			cfg: &Config{
				Main: MainConfig{
					ListenPort: 8080,
					Directory:  "/var/lib/pgwal",
				},
				Receiver: ReceiveConfig{
					Slot: "slot1",
					Uploader: UploadConfig{
						SyncInterval:   "10s",
						MaxConcurrency: 2,
					},
					Retention: RetentionConfig{
						Enable:       true,
						SyncInterval: "12h",
						KeepPeriod:   "24h",
					},
				},
				Storage: StorageConfig{
					Name: StorageNameS3,
					S3: S3Config{
						URL:             "https://s3.amazonaws.com",
						AccessKeyID:     "AKIA...",
						SecretAccessKey: "secret",
						Bucket:          "bucket",
						Region:          "us-east-1",
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid mode and missing main",
			mode: "invalid",
			cfg: &Config{
				Main: MainConfig{},
			},
			expectError: true,
			wantMsgs: []string{
				"invalid mode",
				"main.listen_port is required",
				"main.directory is required",
			},
		},
		{
			name: "invalid uploader and retention durations",
			mode: ModeReceive,
			cfg: &Config{
				Main: MainConfig{
					ListenPort: 1,
					Directory:  "/pgwal",
				},
				Receiver: ReceiveConfig{
					Slot: "slot",
					Uploader: UploadConfig{
						SyncInterval:   "bad",
						MaxConcurrency: 0,
					},
					Retention: RetentionConfig{
						Enable:       true,
						SyncInterval: "nope",
						KeepPeriod:   "never",
					},
				},
				Storage: StorageConfig{
					Name: StorageNameS3,
					S3: S3Config{
						URL:             "x",
						AccessKeyID:     "x",
						SecretAccessKey: "x",
						Bucket:          "x",
						Region:          "x",
					},
				},
			},
			expectError: true,
			wantMsgs: []string{
				"uploader.sync_interval cannot parse",
				"uploader.max_concurrency must be > 0",
				"retention.sync_interval cannot parse",
				"retention.keep_period cannot parse",
			},
		},
		{
			name: "invalid sftp config missing pass or key",
			mode: ModeReceive,
			cfg: &Config{
				Main: MainConfig{
					ListenPort: 1234,
					Directory:  "/data",
				},
				Receiver: ReceiveConfig{
					Slot: "slot",
					Uploader: UploadConfig{
						SyncInterval:   "10s",
						MaxConcurrency: 1,
					},
				},
				Storage: StorageConfig{
					Name: StorageNameSFTP,
					SFTP: SFTPConfig{
						Host: "host",
						Port: 22,
						User: "user",
						// Missing Pass and PKeyPath
					},
				},
			},
			expectError: true,
			wantMsgs: []string{
				"either storage.sftp.pass or storage.sftp.pkey_path must be provided",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.cfg, tt.mode)
			if tt.expectError {
				assert.Error(t, err)
				for _, want := range tt.wantMsgs {
					assert.Contains(t, err.Error(), want)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 10*time.Second, tt.cfg.Receiver.Uploader.SyncIntervalParsed)
			}
		})
	}
}

func TestValidate_SuccessMinimalReceiveConfig(t *testing.T) {
	cfg := &Config{
		Main: MainConfig{
			ListenPort: 8080,
			Directory:  "/var/lib/pgwal",
		},
		Receiver: ReceiveConfig{
			Slot: "replication_slot",
			Uploader: UploadConfig{
				SyncInterval:   "10s",
				MaxConcurrency: 1,
			},
		},
		Storage: StorageConfig{
			Name: "s3",
		},
	}

	err := validate(cfg, ModeReceive)
	assert.Error(t, err)
	assert.Equal(t, 10*time.Second, cfg.Receiver.Uploader.SyncIntervalParsed)
}
