package integration

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/pkg/sftp"

	"github.com/minio/minio-go/v7"
	clients "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"

	"github.com/stretchr/testify/require"
)

func createS3Client() *minio.Client {
	client, err := clients.NewS3Client(&clients.S3Config{
		EndpointURL:     "https://localhost:9000",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin123",
		Bucket:          "backups",
		Region:          "main",
		UsePathStyle:    true,
		DisableSSL:      true,
	})
	if err != nil {
		log.Fatal(err)
	}
	return client.Client()
}

func createSftpClient() *sftp.Client {
	pkeyPath := "./environ/files/dotfiles/.ssh/id_ed25519"
	err := os.Chmod(pkeyPath, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	client, err := clients.NewSFTPClient(&clients.SFTPConfig{
		Host:     "localhost",
		Port:     "2323",
		User:     "testuser",
		PkeyPath: pkeyPath,
	})
	if err != nil {
		log.Fatal(err)
	}
	return client.SFTPClient()
}

func readAllAndClose(t *testing.T, r io.ReadCloser) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return data
}

func genPaths(nested int) string {
	sb := strings.Builder{}
	for i := 0; i < nested; i++ {
		n := rnd(1024, 8192)
		sb.WriteString(fmt.Sprintf("%d/", n))
	}
	return strings.TrimPrefix(sb.String(), "/")
}

func rnd(min, max int) int {
	return rand.Intn(max-min+1) + min
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt64(t *testing.T, key string, fallback int64) int64 {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
