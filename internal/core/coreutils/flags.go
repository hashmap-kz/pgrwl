package coreutils

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type Opts struct {
	Directory    string `json:"directory"`
	Slot         string `json:"slot"`
	NoLoop       bool   `json:"no_loop"`
	LogLevel     string `json:"log_level"`
	LogAddSource bool   `json:"log_add_source"`
	LogFormat    string `json:"log_format"`

	// Optional
	HTTPServerAddr  string `json:"http_server_addr"`
	HTTPServerToken string `json:"http_server_token"`

	setFlags map[string]bool
}

func ParseFlags() (*Opts, error) {
	opts := Opts{
		setFlags: make(map[string]bool),
	}

	flag.StringVar(&opts.Directory, "D", "", "")
	flag.StringVar(&opts.Directory, "directory", "", "")
	flag.StringVar(&opts.Slot, "S", "", "")
	flag.StringVar(&opts.Slot, "slot", "", "")
	flag.BoolVar(&opts.NoLoop, "n", false, "")
	flag.BoolVar(&opts.NoLoop, "no-loop", false, "")
	flag.StringVar(&opts.LogLevel, "log-level", "info", "")
	flag.BoolVar(&opts.LogAddSource, "log-add-source", false, "")
	flag.StringVar(&opts.LogFormat, "log-format", "json", "")
	flag.StringVar(&opts.HTTPServerAddr, "http-server-addr", "", "")
	flag.StringVar(&opts.HTTPServerToken, "http-server-token", "", "")
	flag.Usage = func() {
		_, _ = fmt.Fprintf(os.Stderr, `Usage: pgrwl [OPTIONS]

Main Options:
  -D, --directory       receive write-ahead log files into this directory (required)
  -S, --slot            replication slot to use (required)
  -n, --no-loop         do not loop on connection lost
      --log-level       set log level (trace, debug, info, warn, error) (default: info)
      --log-format      specify log formatter (json, text) (default: json)
      --log-add-source  include source file and line in log output (default: false)

Optional features:
  --http-server-addr    If set, runs an HTTP server in the background (for management, stats, etc.)
                        Example: "--http-server-addr=:8080"
  --http-server-token   Required when the HTTP server is enabled (treat this as a password)
                        Example: "--http-server-token=${MY_SECRET_PASS}"
`)
	}
	flag.Parse()

	// Track explicitly passed flags
	flag.Visit(func(f *flag.Flag) {
		opts.setFlags[f.Name] = true
	})
	setStringFromFlagOrEnv(opts.setFlags, []string{"directory", "D"}, &opts.Directory, "PGRWL_DIRECTORY")
	setStringFromFlagOrEnv(opts.setFlags, []string{"slot", "S"}, &opts.Slot, "PGRWL_SLOT")
	setBoolFromFlagOrEnv(opts.setFlags, []string{"no-loop", "n"}, &opts.NoLoop, "PGRWL_NO_LOOP")
	setStringFromFlagOrEnv(opts.setFlags, []string{"log-level"}, &opts.LogLevel, "PGRWL_LOG_LEVEL")
	setStringFromFlagOrEnv(opts.setFlags, []string{"log-format"}, &opts.LogFormat, "PGRWL_LOG_FORMAT")
	setBoolFromFlagOrEnv(opts.setFlags, []string{"log-add-source"}, &opts.LogAddSource, "PGRWL_LOG_ADD_SOURCE")

	setStringFromFlagOrEnv(opts.setFlags, []string{"http-server-addr"}, &opts.HTTPServerAddr, "PGRWL_HTTP_SERVER_ADDR")
	setStringFromFlagOrEnv(opts.setFlags, []string{"http-server-token"}, &opts.HTTPServerToken, "PGRWL_HTTP_SERVER_TOKEN")

	if opts.Directory == "" {
		return nil, fmt.Errorf("directory is not specified")
	}
	if opts.Slot == "" {
		return nil, fmt.Errorf("replication slot name is not specified")
	}

	// set env-vars
	_ = os.Setenv("LOG_LEVEL", opts.LogLevel)
	_ = os.Setenv("LOG_FORMAT", opts.LogFormat)
	if opts.LogAddSource {
		_ = os.Setenv("LOG_ADD_SOURCE", "1")
	}

	// check connstr vars are set

	requiredVars := []string{
		"PGHOST",
		"PGPORT",
		"PGUSER",
		"PGPASSWORD",
	}
	var empty []string
	for _, v := range requiredVars {
		if os.Getenv(v) == "" {
			empty = append(empty, v)
		}
	}
	if len(empty) > 0 {
		return nil, fmt.Errorf("required vars are empty: [%s]", strings.Join(empty, " "))
	}
	return &opts, nil
}

func setStringFromFlagOrEnv(setFlags map[string]bool, flagNames []string, target *string, envVar string) {
	for _, name := range flagNames {
		if setFlags[name] {
			return // CLI flag was explicitly set
		}
	}
	val := os.Getenv(envVar)
	if val != "" {
		*target = val
	}
}

func setBoolFromFlagOrEnv(setFlags map[string]bool, flagNames []string, target *bool, envVar string) {
	for _, flagName := range flagNames {
		if setFlags[flagName] {
			// leave as-is: explicitly set via CLI
			return
		}
	}
	env := os.Getenv(envVar)
	if env == "" {
		return // no override
	}
	//nolint:errcheck
	v, _ := parseBool(env)
	*target = v
}

// parseBool parses a string into a boolean value.
// 1, true, t, yes, on (case-insensitive) -> true
// 0, false, f, no, off (case-insensitive) -> false
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "t", "yes", "on":
		return true, nil
	case "0", "false", "f", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %q", s)
	}
}
