//nolint:revive
package shared

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/clients"
	st "github.com/hashmap-kz/storecrypt/pkg/storage"
	"github.com/hashmap-kz/streamcrypt/pkg/codec"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt/aesgcm"
)

type SetupStorageOpts struct {
	BaseDir string
	SubPath string // for localfs storage, or basebackups
}

func SetupStorage(opts *SetupStorageOpts) (*st.VariadicStorage, error) {
	cfg := config.Cfg()

	// storage configs
	alg := st.Algorithms{
		Gzip: &st.CodecPair{
			Compressor:   codec.GzipCompressor{},
			Decompressor: codec.GzipDecompressor{},
		},
		Zstd: &st.CodecPair{
			Compressor:   codec.ZstdCompressor{},
			Decompressor: codec.ZstdDecompressor{},
		},
		AES: aesgcm.NewChunkedGCMCrypter(cfg.Storage.Encryption.Pass),
	}
	writeExt := getWriteExt(cfg)

	baseDir := filepath.ToSlash(opts.BaseDir)
	if strings.TrimSpace(opts.SubPath) != "" {
		baseDir = filepath.ToSlash(filepath.Join(opts.BaseDir, opts.SubPath))
	}

	// localFS by default
	if cfg.IsLocalStor() {
		if opts.SubPath == "" {
			return nil, fmt.Errorf("for localfs storage subpath is required")
		}
		backend, err := st.NewLocal(&st.LocalStorageOpts{
			BaseDir:      baseDir,
			FsyncOnWrite: true,
		})
		if err != nil {
			return nil, err
		}
		return st.NewVariadicStorage(backend, alg, writeExt)
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
		backend := st.NewSFTPStorage(client.SFTPClient(), remotePath)
		return st.NewVariadicStorage(backend, alg, writeExt)
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
		backend := st.NewS3Storage(client.Client(), cfg.Storage.S3.Bucket, baseDir)
		return st.NewVariadicStorage(backend, alg, writeExt)
	}

	return nil, fmt.Errorf("unknown storage name: %s", cfg.Storage.Name)
}

func getWriteExt(cfg *config.Config) string {
	enc := ""
	if cfg.Storage.Encryption.Algo != "" {
		if cfg.Storage.Encryption.Algo == config.RepoEncryptorAes256Gcm {
			enc = ".aes"
		}
	}
	com := ""
	if cfg.Storage.Compression.Algo != "" {
		if cfg.Storage.Compression.Algo == config.RepoCompressorZstd {
			com = ".zst"
		}
		if cfg.Storage.Compression.Algo == config.RepoCompressorGzip {
			com = ".gz"
		}
	}
	writeExt := fmt.Sprintf("%s%s", com, enc)
	return writeExt
}
