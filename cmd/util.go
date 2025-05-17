package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

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

	// TODO: handle compression/encryption configs

	if cfg.Storage.Name == "" || cfg.Storage.Name == config.StorageNameLocal {
		local, err := st.NewLocal(&st.LocalStorageOpts{
			BaseDir: baseDir,
		})
		if err != nil {
			return nil, err
		}
		return &st.TransformingStorage{
			Backend: local,
		}, nil
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
			Crypter:      nil,
			Compressor:   nil,
			Decompressor: nil,
		}, nil
	}

	return nil, fmt.Errorf("unknown storage name: %s", cfg.Storage.Name)
}
