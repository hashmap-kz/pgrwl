package config

import (
	"os"
	"path/filepath"
	"testing"

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

func TestLoadCfg_FileAndEnvMerge(t *testing.T) {
	// Set env vars that will override or fill missing values
	_ = os.Setenv("PGRWL_SLOT", "env_slot")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "6000")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")
	_ = os.Setenv("PGRWL_S3_DISABLE_SSL", "true")
	_ = os.Setenv("PGRWL_S3_URL", "http://env-url")

	json := `{
		"PGRWL_MODE": "receive",
		"PGRWL_DIRECTORY": "/var/lib/test",
		"PGRWL_NO_LOOP": true,
		"PGRWL_LOG_LEVEL": "info"
	}`

	path := writeTempConfigFile(t, json)

	cfg, err := loadCfg(path)
	assert.NoError(t, err)

	assert.Equal(t, "receive", cfg.Mode)
	assert.Equal(t, "/var/lib/test", cfg.Directory)
	assert.Equal(t, true, cfg.NoLoop)
	assert.Equal(t, "info", cfg.LogLevel)

	// These come from env
	assert.Equal(t, "env_slot", cfg.Slot)
	assert.Equal(t, 6000, cfg.ListenPort)
	assert.Equal(t, true, cfg.LogAddSource)
	assert.Equal(t, true, cfg.S3DisableSSL)
	assert.Equal(t, "http://env-url", cfg.S3URL)
}

func TestLoadCfg_NoFile_OnlyEnv(t *testing.T) {
	_ = os.Setenv("PGRWL_MODE", "env_mode")
	_ = os.Setenv("PGRWL_NO_LOOP", "true")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "7777")

	cfg, err := loadCfg("") // no file
	assert.NoError(t, err)

	assert.Equal(t, "env_mode", cfg.Mode)
	assert.True(t, cfg.NoLoop)
	assert.Equal(t, 7777, cfg.ListenPort)
}

func TestLoadCfg_FromFile(t *testing.T) {
	jsonData := `{
		"PGRWL_MODE": "receive",
		"PGRWL_DIRECTORY": "/tmp/test",
		"PGRWL_SLOT": "myslot",
		"PGRWL_NO_LOOP": true,
		"PGRWL_LISTEN_PORT": 5432,
		"PGRWL_LOG_LEVEL": "debug",
		"PGRWL_LOG_FORMAT": "text",
		"PGRWL_LOG_ADD_SOURCE": true
	}`
	path := writeTempConfigFile(t, jsonData)

	// Reset singleton for testing
	reset()

	cfg := Read(path)

	assert.Equal(t, "receive", cfg.Mode)
	assert.Equal(t, "/tmp/test", cfg.Directory)
	assert.Equal(t, "myslot", cfg.Slot)
	assert.True(t, cfg.NoLoop)
	assert.Equal(t, 5432, cfg.ListenPort)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.True(t, cfg.LogAddSource)
}

func TestLoadCfg_FromEnvFallback(t *testing.T) {
	os.Clearenv()

	_ = os.Setenv("PGRWL_SLOT", "fallback_slot")
	_ = os.Setenv("PGRWL_LOG_ADD_SOURCE", "true")

	// Reset singleton for testing
	reset()

	cfg := Read("") // No file

	assert.Equal(t, "fallback_slot", cfg.Slot)
	assert.Equal(t, true, cfg.LogAddSource)
	assert.Equal(t, "", cfg.Mode) // not set in env or file
}

func TestMergeEnvIfUnset(t *testing.T) {
	_ = os.Setenv("PGRWL_MODE", "env-mode")
	_ = os.Setenv("PGRWL_LISTEN_PORT", "1234")

	cfg := Config{
		LogLevel: "warn", // should not be overwritten
	}

	mergeEnvIfUnset(&cfg)

	assert.Equal(t, "env-mode", cfg.Mode)
	assert.Equal(t, 1234, cfg.ListenPort)
	assert.Equal(t, "warn", cfg.LogLevel)
}
