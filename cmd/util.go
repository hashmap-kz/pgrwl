package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashmap-kz/streamcrypt/pkg/codec"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt"
	"github.com/hashmap-kz/streamcrypt/pkg/crypt/aesgcm"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/storecrypt/pkg/clients"
	st "github.com/hashmap-kz/storecrypt/pkg/storage"
)

// HTTP

func addr(from string) (string, error) {
	if strings.HasPrefix(from, "http://") || strings.HasPrefix(from, "https://") {
		return from, nil
	}
	host, port, err := net.SplitHostPort(from)
	if err != nil {
		return "", err
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s", host, port), nil
}

func runHTTPServer(ctx context.Context, port int, router http.Handler) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           router,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Context was cancelled, shut down the HTTP server gracefully
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", slog.Any("err", err))
		} else {
			slog.Debug("HTTP server shut down")
		}
	}()

	slog.Info("starting HTTP server", slog.String("addr", srv.Addr))

	// Start the server (blocking)
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err // real error
	}
	return nil
}

func setupStorage(baseDir string) (*st.TransformingStorage, error) {
	cfg := config.Cfg()

	compressor, decompressor, crypter, err := decideCompressorEncryptor(cfg)
	if err != nil {
		return nil, err
	}

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
			slog.String("crypter", string(cfg.Storage.Encryption.Algo)),
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

func checkStorageManifest(cfg *config.Config, dir string) (*StorageManifest, error) {
	var m StorageManifest
	manifestPath := filepath.Join(dir, "manifest.json")
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
			err = os.WriteFile(manifestPath, data, 0o640)
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
