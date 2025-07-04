package shared

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/clients"
	st "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/hashmap-kz/streamcrypt/pkg/codec"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt/aesgcm"
)

type SetupStorageOpts struct {
	BaseDir string
	SubPath string // for localfs storage, or basebackups
}

func SetupStorage(opts *SetupStorageOpts) (*st.TransformingStorage, error) {
	cfg := config.Cfg()

	baseDir := filepath.ToSlash(opts.BaseDir)
	if strings.TrimSpace(opts.SubPath) != "" {
		baseDir = filepath.ToSlash(filepath.Join(opts.BaseDir, opts.SubPath))
	}

	compressor, decompressor, crypter, err := decideCompressorEncryptor(cfg)
	if err != nil {
		return nil, err
	}

	// localFS by default
	if cfg.IsLocalStor() {
		if opts.SubPath == "" {
			return nil, fmt.Errorf("for localfs storage subpath is required")
		}
		local, err := st.NewLocal(&st.LocalStorageOpts{
			BaseDir:      baseDir,
			FsyncOnWrite: true,
		})
		if err != nil {
			return nil, err
		}
		return &st.TransformingStorage{
			Backend:      local,
			Crypter:      crypter,
			Compressor:   compressor,
			Decompressor: decompressor,
		}, nil
	}

	// sftp
	if strings.EqualFold(cfg.Storage.Name, config.StorageNameSFTP) {
		client, err := clients.NewSFTPClient(&clients.SFTPConfig{
			Host:       cfg.Storage.SFTP.Host,
			Port:       fmt.Sprintf("%d", cfg.Storage.SFTP.Port),
			User:       cfg.Storage.SFTP.User,
			PkeyPath:   cfg.Storage.SFTP.PKeyPath,
			Passphrase: cfg.Storage.SFTP.PKeyPass,
		})
		if err != nil {
			return nil, err
		}
		remotePath := filepath.ToSlash(filepath.Join(cfg.Storage.SFTP.BaseDir, baseDir))
		return &st.TransformingStorage{
			Backend:      st.NewSFTPStorage(client.SFTPClient(), remotePath),
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
