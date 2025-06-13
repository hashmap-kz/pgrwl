package cmd

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/stretchr/testify/assert"
)

func TestNeedSupervisorLoop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))

	t.Run("localfs-1 with no compression, encryption, retention", func(t *testing.T) {
		cfg := &config.Config{
			Receiver: config.ReceiveConfig{
				Retention: config.RetentionConfig{
					Enable: false,
				},
			},
			Storage: config.StorageConfig{
				Name: "local",
				Compression: config.CompressionConfig{
					Algo: "",
				},
				Encryption: config.EncryptionConfig{
					Algo: "",
				},
			},
		}
		assert.False(t, needSupervisorLoop(cfg, logger))
	})

	t.Run("localfs-2 with no compression, encryption, retention", func(t *testing.T) {
		cfg := &config.Config{
			Receiver: config.ReceiveConfig{
				Retention: config.RetentionConfig{
					Enable: false,
				},
			},
			Storage: config.StorageConfig{
				Name: "",
				Compression: config.CompressionConfig{
					Algo: "",
				},
				Encryption: config.EncryptionConfig{
					Algo: "",
				},
			},
		}
		assert.False(t, needSupervisorLoop(cfg, logger))
	})

	t.Run("localfs with compression enabled", func(t *testing.T) {
		cfg := &config.Config{
			Storage: config.StorageConfig{
				Name: "local",
				Compression: config.CompressionConfig{
					Algo: "gzip",
				},
			},
		}
		assert.True(t, needSupervisorLoop(cfg, logger))
	})

	t.Run("localfs with encryption enabled", func(t *testing.T) {
		cfg := &config.Config{
			Storage: config.StorageConfig{
				Name: "local",
				Encryption: config.EncryptionConfig{
					Algo: "aes-256-gcm",
				},
			},
		}
		assert.True(t, needSupervisorLoop(cfg, logger))
	})

	t.Run("localfs with retention enabled", func(t *testing.T) {
		cfg := &config.Config{
			Receiver: config.ReceiveConfig{
				Retention: config.RetentionConfig{
					Enable: true,
				},
			},
			Storage: config.StorageConfig{
				Name: "local",
			},
		}
		assert.True(t, needSupervisorLoop(cfg, logger))
	})

	t.Run("non-local storage", func(t *testing.T) {
		cfg := &config.Config{
			Storage: config.StorageConfig{
				Name: "s3",
			},
		}
		assert.True(t, needSupervisorLoop(cfg, logger))
	})
}
