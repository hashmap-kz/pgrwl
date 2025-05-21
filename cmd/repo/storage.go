package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/clients"
	st "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/hashmap-kz/streamcrypt/pkg/codec"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt/aesgcm"
)

type StorageManifest struct {
	CompressionAlgo string `json:"compression_algo,omitempty"`
	EncryptionAlgo  string `json:"encryption_algo,omitempty"`
}

func SetupStorage(baseDir string) (*st.TransformingStorage, error) {
	cfg := config.Cfg()

	compressor, decompressor, crypter, err := decideCompressorEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	// sftp
	if strings.EqualFold(cfg.Storage.Name, config.StorageNameSFTP) {
		client, err := clients.NewSFTPClient(&clients.SFTPConfig{
			Host:       cfg.Storage.SFTP.Host,
			Port:       fmt.Sprintf("%d", cfg.Storage.SFTP.Port),
			User:       cfg.Storage.SFTP.User,
			PkeyPath:   cfg.Storage.SFTP.Pass,
			Passphrase: cfg.Storage.SFTP.PKeyPass,
		})
		if err != nil {
			return nil, err
		}
		return &st.TransformingStorage{
			Backend:      st.NewSFTPStorage(client.SFTPClient(), baseDir),
			Crypter:      crypter,
			Compressor:   compressor,
			Decompressor: decompressor,
		}, nil
	}

	// s3
	if strings.EqualFold(cfg.Storage.Name, config.StorageNameS3) {
		client, err := clients.NewS3Client(&clients.S3Config{
			EndpointURL:     cfg.Storage.S3.URL,
			AccessKeyID:     cfg.Storage.S3.AccessKeyID,
			SecretAccessKey: cfg.Storage.S3.SecretAccessKey,
			Bucket:          cfg.Storage.S3.Bucket,
			Region:          cfg.Storage.S3.Region,
			UsePathStyle:    cfg.Storage.S3.UsePathStyle,
			DisableSSL:      cfg.Storage.S3.DisableSSL,
		})
		if err != nil {
			return nil, err
		}
		return &st.TransformingStorage{
			Backend:      st.NewS3Storage(client.Client(), cfg.Storage.S3.Bucket, baseDir),
			Crypter:      crypter,
			Compressor:   compressor,
			Decompressor: decompressor,
		}, nil
	}

	return nil, fmt.Errorf("unknown storage name: %s", cfg.Storage.Name)
}

func decideCompressorEncryptor(cfg *config.Config) (codec.Compressor, codec.Decompressor, crypt.Crypter, error) {
	var compressor codec.Compressor
	var decompressor codec.Decompressor
	var crypter crypt.Crypter

	if cfg.Storage.Compression.Algo != "" {
		slog.Info("init compressor",
			slog.String("module", "boot"),
			slog.String("compressor", cfg.Storage.Compression.Algo),
		)

		switch cfg.Storage.Compression.Algo {
		case config.RepoCompressorGzip:
			compressor = &codec.GzipCompressor{}
			decompressor = &codec.GzipDecompressor{}
		case config.RepoCompressorZstd:
			compressor = &codec.ZstdCompressor{}
			decompressor = codec.ZstdDecompressor{}
		default:
			return nil, nil, nil,
				fmt.Errorf("unknown compression algo: %s", cfg.Storage.Compression.Algo)
		}
	}
	if cfg.Storage.Encryption.Algo != "" {
		slog.Info("init crypter",
			slog.String("module", "boot"),
			slog.String("crypter", cfg.Storage.Encryption.Algo),
		)

		if cfg.Storage.Encryption.Algo == config.RepoEncryptorAes256Gcm {
			crypter = aesgcm.NewChunkedGCMCrypter(cfg.Storage.Encryption.Pass)
		} else {
			return nil, nil, nil,
				fmt.Errorf("unknown encryption algo: %s", cfg.Storage.Encryption.Algo)
		}
	}

	return compressor, decompressor, crypter, nil
}

// manifest

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
