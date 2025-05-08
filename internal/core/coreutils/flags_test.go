package coreutils

import (
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

func TestParseBool(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
		hasErr   bool
	}{
		{"1", true, false},
		{"true", true, false},
		{"t", true, false},
		{"yes", true, false},
		{"on", true, false},
		{"0", false, false},
		{"false", false, false},
		{"f", false, false},
		{"no", false, false},
		{"off", false, false},
		{"invalid", false, true},
	}

	for _, c := range cases {
		got, err := parseBool(c.input)
		if c.hasErr {
			assert.Error(t, err, "expected error for input %q", c.input)
		} else {
			assert.NoError(t, err, "did not expect error for input %q", c.input)
			assert.Equal(t, c.expected, got)
		}
	}
}

func TestSetStringFromFlagOrEnv(t *testing.T) {
	setFlags := map[string]bool{}
	target := ""
	_ = os.Setenv("TEST_ENV_STR", "env_value")
	setStringFromFlagOrEnv(setFlags, []string{"flag1"}, &target, "TEST_ENV_STR")
	assert.Equal(t, "env_value", target)
}

func TestSetBoolFromFlagOrEnv(t *testing.T) {
	setFlags := map[string]bool{}
	target := false
	_ = os.Setenv("TEST_ENV_BOOL", "true")
	setBoolFromFlagOrEnv(setFlags, []string{"flag2"}, &target, "TEST_ENV_BOOL")
	assert.True(t, target)
}

func TestParseFlags_CLIOverridesEnv(t *testing.T) {
	resetFlags()
	_ = os.Setenv("PGRWL_DIRECTORY", "env_dir")
	_ = os.Setenv("PGRWL_SLOT", "env_slot")
	_ = os.Setenv("PGHOST", "localhost")
	_ = os.Setenv("PGPORT", "5432")
	_ = os.Setenv("PGUSER", "postgres")
	_ = os.Setenv("PGPASSWORD", "secret")

	os.Args = []string{"cmd", "--directory=cli_dir", "--slot=cli_slot"}
	opts, err := ParseFlags()
	assert.NoError(t, err)
	assert.Equal(t, "cli_dir", opts.Directory)
	assert.Equal(t, "cli_slot", opts.Slot)
}

func TestParseFlags_EnvFallback(t *testing.T) {
	resetFlags()
	_ = os.Setenv("PGRWL_DIRECTORY", "env_dir")
	_ = os.Setenv("PGRWL_SLOT", "env_slot")
	_ = os.Setenv("PGHOST", "localhost")
	_ = os.Setenv("PGPORT", "5432")
	_ = os.Setenv("PGUSER", "postgres")
	_ = os.Setenv("PGPASSWORD", "secret")

	os.Args = []string{"cmd"}
	opts, err := ParseFlags()
	assert.NoError(t, err)
	assert.Equal(t, "env_dir", opts.Directory)
	assert.Equal(t, "env_slot", opts.Slot)
}

func TestParseFlags_MissingRequiredVars(t *testing.T) {
	resetFlags()
	os.Clearenv()
	_ = os.Setenv("PGRWL_DIRECTORY", "env_dir")
	_ = os.Setenv("PGRWL_SLOT", "env_slot")
	os.Args = []string{"cmd"}

	opts, err := ParseFlags()
	assert.Nil(t, opts)
	assert.ErrorContains(t, err, "required vars are empty")
}
