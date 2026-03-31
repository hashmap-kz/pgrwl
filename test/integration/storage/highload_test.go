//go:build integration_storage_highload

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"testing"

	storage "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/shared/streamcrypt/codec"
	"github.com/pgrwl/pgrwl/internal/opt/shared/streamcrypt/crypt/aesgcm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func impls(dir, subpath string) map[string]storage.Storage {
	// Helpers to create *isolated* backends per storage name.
	mkLocal := func(name string) storage.Storage {
		s, err := storage.NewLocal(&storage.LocalStorageOpts{
			BaseDir:      filepath.Join(dir, subpath, name),
			FsyncOnWrite: false,
		})
		if err != nil {
			log.Fatal(err)
		}
		return s
	}

	mkS3 := func(name string) storage.Storage {
		return storage.NewS3Storage(
			createS3Client(),
			"backups",
			filepath.Join(subpath, name),
		)
	}

	mkSFTP := func(name string) storage.Storage {
		return storage.NewSFTPStorage(
			createSftpClient(),
			filepath.Join(subpath, name),
		)
	}

	// Algorithms: tell VariadicStorage what we *can* do.
	alg := storage.Algorithms{
		Gzip: &storage.CodecPair{
			Compressor:   codec.GzipCompressor{},
			Decompressor: codec.GzipDecompressor{},
		},
		Zstd: &storage.CodecPair{
			Compressor:   codec.ZstdCompressor{},
			Decompressor: codec.ZstdDecompressor{},
		},
		AES: aesgcm.NewChunkedGCMCrypter("password"),
	}

	newVariadic := func(backend storage.Storage, writeExt string) storage.Storage {
		st, err := storage.NewVariadicStorage(backend, alg, writeExt)
		if err != nil {
			log.Fatalf("failed to create VariadicStorage: %v", err)
		}
		return st
	}

	return map[string]storage.Storage{
		// dynamic – **each has its own backend** too
		"dyn-local-gz.aes": newVariadic(mkLocal("dyn-local-gz.aes"), ".gz.aes"),
		"dyn-s3-gz.aes":    newVariadic(mkS3("dyn-s3-gz.aes"), ".gz.aes"),
		"dyn-sftp-gz.aes":  newVariadic(mkSFTP("dyn-sftp-gz.aes"), ".gz.aes"),

		"dyn-local-gz": newVariadic(mkLocal("dyn-local-gz"), ".gz"),
		"dyn-s3-gz":    newVariadic(mkS3("dyn-s3-gz"), ".gz"),
		"dyn-sftp-gz":  newVariadic(mkSFTP("dyn-sftp-gz"), ".gz"),

		"dyn-local": newVariadic(mkLocal("dyn-local"), ""),
		"dyn-s3":    newVariadic(mkS3("dyn-s3"), ""),
		"dyn-sftp":  newVariadic(mkSFTP("dyn-sftp"), ""),

		"dyn-local-zst": newVariadic(mkLocal("dyn-local-zst"), ".zst"),
		"dyn-s3-zst":    newVariadic(mkS3("dyn-s3-zst"), ".zst"),
		"dyn-sftp-zst":  newVariadic(mkSFTP("dyn-sftp-zst"), ".zst"),

		"dyn-local-aes": newVariadic(mkLocal("dyn-local-aes"), ".aes"),
		"dyn-s3-aes":    newVariadic(mkS3("dyn-s3-aes"), ".aes"),
		"dyn-sftp-aes":  newVariadic(mkSFTP("dyn-sftp-aes"), ".aes"),
	}
}

func initStoragesT(t *testing.T, subpath string) map[string]storage.Storage {
	t.Helper()
	return impls(t.TempDir(), subpath)
}

func TestStorage_HighLoad2000(t *testing.T) {
	ctx := context.TODO()
	storages := initStoragesT(t, t.Name())

	for name, store := range storages {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.DeleteAll(ctx, ""), "[%s] DeleteAll before test", name)

			const fileCount = 2000
			content := []byte("this is a stress test file content")

			// Upload
			for i := 0; i < fileCount; i++ {
				path := fmt.Sprintf("hl/%04d.txt", i)
				err := store.Put(ctx, path, bytes.NewReader(content))
				require.NoError(t, err, "[%s] Put failed for %s", name, path)
			}

			// List and check count
			list, err := store.List(ctx, "hl")
			require.NoError(t, err, "[%s] List failed", name)
			assert.Len(t, list, fileCount, "[%s] List returned wrong count", name)

			// Read and verify
			for i := 0; i < fileCount; i++ {
				path := fmt.Sprintf("hl/%04d.txt", i)

				exists, err := store.Exists(ctx, path)
				require.NoError(t, err, "[%s] Exists failed for %s", name, path)
				assert.True(t, exists, "[%s] File missing: %s", name, path)

				r, err := store.Get(ctx, path)
				require.NoError(t, err, "[%s] Get failed for %s", name, path)

				read, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				assert.Equal(t, content, read, "[%s] Content mismatch for %s", name, path)
			}

			// Cleanup
			require.NoError(t, store.DeleteAll(ctx, "hl"), "[%s] DeleteAll cleanup failed", name)
			finalList, err := store.List(ctx, "hl")
			require.NoError(t, err)
			assert.Empty(t, finalList, "[%s] Files not cleaned up", name)
		})
	}
}
