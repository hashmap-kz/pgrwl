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
	_ = os.Unsetenv("PGRWL_MODE")
	_ = os.Unsetenv("PGRWL_DIRECTORY")
	_ = os.Unsetenv("PGRWL_RECEIVE_SLOT")
	_ = os.Unsetenv("PGRWL_RECEIVE_NO_LOOP")
	_ = os.Unsetenv("PGRWL_RECEIVE_LISTEN_PORT")
	_ = os.Unsetenv("PGRWL_RESTORE_FETCH_ADDR")
	_ = os.Unsetenv("PGRWL_LOG_LEVEL")
	_ = os.Unsetenv("PGRWL_LOG_FORMAT")
	_ = os.Unsetenv("PGRWL_LOG_ADD_SOURCE")
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
	_ = os.Setenv("PGRWL_RECEIVE_LISTEN_PORT", "6000")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")
	_ = os.Setenv("PGRWL_S3_DISABLE_SSL", "true")
	_ = os.Setenv("PGRWL_S3_URL", "http://env-url")

	json := `{
		"PGRWL_MODE": "receive",
		"PGRWL_DIRECTORY": "/var/lib/test",
		"PGRWL_RECEIVE_NO_LOOP": true,
		"PGRWL_LOG_LEVEL": "info"
	}`

	path := writeTempConfigFile(t, json)

	cfg, err := loadCfg(path)
	assert.NoError(t, err)

	assert.Equal(t, "receive", cfg.Mode)
	assert.Equal(t, "/var/lib/test", cfg.Directory)
	assert.Equal(t, true, cfg.ReceiveNoLoop)
	assert.Equal(t, "info", cfg.LogLevel)

	// These come from env
	assert.Equal(t, "env_slot", cfg.ReceiveSlot)
	assert.Equal(t, 6000, cfg.ReceiveListenPort)
	assert.Equal(t, true, cfg.LogAddSource)
	assert.Equal(t, true, cfg.S3DisableSSL)
	assert.Equal(t, "http://env-url", cfg.S3URL)
}

func TestLoadCfg_NoFile_OnlyEnv(t *testing.T) {
	cleanenvs(t)

	_ = os.Setenv("PGRWL_MODE", "env_mode")
	_ = os.Setenv("PGRWL_RECEIVE_NO_LOOP", "true")
	_ = os.Setenv("PGRWL_RECEIVE_LISTEN_PORT", "7777")

	cfg, err := loadCfg("") // no file
	assert.NoError(t, err)

	assert.Equal(t, "env_mode", cfg.Mode)
	assert.True(t, cfg.ReceiveNoLoop)
	assert.Equal(t, 7777, cfg.ReceiveListenPort)
}

func TestLoadCfg_FromFile(t *testing.T) {
	cleanenvs(t)

	jsonData := `{
		"PGRWL_MODE": "receive",
		"PGRWL_DIRECTORY": "/tmp/test",
		"PGRWL_RECEIVE_SLOT": "myslot",
		"PGRWL_RECEIVE_NO_LOOP": true,
		"PGRWL_RECEIVE_LISTEN_PORT": 5432,
		"PGRWL_LOG_LEVEL": "debug",
		"PGRWL_LOG_FORMAT": "text",
		"PGRWL_LOG_ADD_SOURCE": true
	}`
	path := writeTempConfigFile(t, jsonData)

	cfg, err := loadCfg(path)
	require.NoError(t, err)

	assert.Equal(t, "receive", cfg.Mode)
	assert.Equal(t, "/tmp/test", cfg.Directory)
	assert.Equal(t, "myslot", cfg.ReceiveSlot)
	assert.True(t, cfg.ReceiveNoLoop)
	assert.Equal(t, 5432, cfg.ReceiveListenPort)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.True(t, cfg.LogAddSource)
}

func TestLoadCfg_FromEnvFallback(t *testing.T) {
	cleanenvs(t)

	_ = os.Setenv("PGRWL_RECEIVE_SLOT", "fallback_slot")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")

	cfg, err := loadCfg("") // No file
	require.NoError(t, err)

	assert.Equal(t, "fallback_slot", cfg.ReceiveSlot)
	assert.Equal(t, true, cfg.LogAddSource)
	assert.Equal(t, "", cfg.Mode) // not set in env or file
}

func TestMergeEnvIfUnset(t *testing.T) {
	cleanenvs(t)

	_ = os.Setenv("PGRWL_MODE", "env-mode")
	_ = os.Setenv("PGRWL_RECEIVE_LISTEN_PORT", "1234")

	cfg := Config{
		LogLevel: "warn", // should not be overwritten
	}

	mergeEnvIfUnset(&cfg)

	assert.Equal(t, "env-mode", cfg.Mode)
	assert.Equal(t, 1234, cfg.ReceiveListenPort)
	assert.Equal(t, "warn", cfg.LogLevel)
}
