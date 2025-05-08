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
		_, _ = fmt.Fprintf(os.Stderr, `Run WAL-receiver

Examples:
  # Set PostgreSQL connection parameters
  export PGHOST='localhost'
  export PGPORT='5432'
  export PGUSER='postgres'
  export PGPASSWORD='postgres'

  # Start receiving WAL files into local directory '/mnt/wal-archive'
  # using the replication slot named 'bookstore_app'
  pgrwl -D /mnt/wal-archive -S bookstore_app

Main Options:
  -D, --directory='':
      Target directory to store WAL files. Required.
      ENV: PGRWL_DIRECTORY

  -S, --slot='':
      Replication slot to use. Required.
      ENV: PGRWL_SLOT

  -n, --no-loop=false:
      If set, do not loop on connection loss.
      ENV: PGRWL_NO_LOOP

Logging:
  --log-level=info:
      Set log verbosity: trace, debug, info, warn, error.
      ENV: PGRWL_LOG_LEVEL

  --log-format=json:
      Specify log formatter: json or text.
      ENV: PGRWL_LOG_FORMAT

  --log-add-source=false:
      Include file and line info in logs if set.
      ENV: PGRWL_LOG_ADD_SOURCE

Optional HTTP Server:
  --http-server-addr='':
      Run a background HTTP server for monitoring and control.
      ENV: PGRWL_HTTP_SERVER_ADDR

  --http-server-token='':
      Token required for accessing the HTTP server.
      ENV: PGRWL_HTTP_SERVER_TOKEN

Help:
  -h, --help:
      Show this help message and exit.

Usage:
  pgrwl -D DIRECTORY -S SLOT [options]
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
