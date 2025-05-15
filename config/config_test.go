package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func writeTempConfigFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(tmpFile, []byte(content), 0o644)
	assert.NoError(t, err)
	return tmpFile
}

func cleanenvs(t *testing.T) {
	t.Helper()
	_ = os.Unsetenv("PGRWL_STORAGE_TYPE")
	_ = os.Unsetenv("PGRWL_STORAGE_COMPRESSION_ALGO")
	_ = os.Unsetenv("PGRWL_STORAGE_ENCRYPTION_ALGO")
	_ = os.Unsetenv("PGRWL_STORAGE_ENCRYPTION_PASS")
	_ = os.Unsetenv("PGRWL_SFTP_HOST")
	_ = os.Unsetenv("PGRWL_SFTP_PORT")
	_ = os.Unsetenv("PGRWL_SFTP_USER")
	_ = os.Unsetenv("PGRWL_SFTP_PASS")
	_ = os.Unsetenv("PGRWL_SFTP_PKEY_PATH")
	_ = os.Unsetenv("PGRWL_SFTP_PKEY_PASS")
	_ = os.Unsetenv("PGRWL_S3_URL")
	_ = os.Unsetenv("PGRWL_S3_ACCESS_KEY_ID")
	_ = os.Unsetenv("PGRWL_S3_SECRET_ACCESS_KEY")
	_ = os.Unsetenv("PGRWL_S3_BUCKET")
	_ = os.Unsetenv("PGRWL_S3_REGION")
	_ = os.Unsetenv("PGRWL_S3_USE_PATH_STYLE")
	_ = os.Unsetenv("PGRWL_S3_DISABLE_SSL")
}

func TestLoadCfg_FileAndEnvMerge(t *testing.T) {
	cleanenvs(t)

	// Set env vars that will override or fill missing values
	_ = os.Setenv("PGRWL_RECEIVE_SLOT", "env_slot")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "6000")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")
	_ = os.Setenv("PGRWL_S3_DISABLE_SSL", "true")
	_ = os.Setenv("PGRWL_S3_URL", "http://env-url")

	jsonData := `{
		"PGRWL_DIRECTORY": "/var/lib/test",
		"PGRWL_RECEIVE_NO_LOOP": true,
		"PGRWL_LOG_LEVEL": "info"
	}`

	path := writeTempConfigFile(t, jsonData)

	cfg, err := mustLoadCfg(path)
	assert.NoError(t, err)

	// These come from env
	assert.Equal(t, true, cfg.S3DisableSSL)
	assert.Equal(t, "http://env-url", cfg.S3URL)
}

func TestLoadCfg_FromFile(t *testing.T) {
	cleanenvs(t)

	jsonData := `{
		"PGRWL_DIRECTORY": "/tmp/test",
		"PGRWL_RECEIVE_SLOT": "myslot",
		"PGRWL_RECEIVE_NO_LOOP": true,
		"PGRWL_LISTEN_PORT": 5432,
		"PGRWL_LOG_LEVEL": "debug",
		"PGRWL_LOG_FORMAT": "text",
		"PGRWL_LOG_ADD_SOURCE": true
	}`
	path := writeTempConfigFile(t, jsonData)

	_, err := mustLoadCfg(path)
	require.NoError(t, err)
}

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
