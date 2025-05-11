package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

func applyStringFallback(f *pflag.FlagSet, name string, target *string, envKey string) {
	if !f.Changed(name) {
		if val := os.Getenv(envKey); val != "" {
			*target = val
		}
	}
}

func applyBoolFallback(f *pflag.FlagSet, name string, target *bool, envKey string) {
	if !f.Changed(name) {
		if val := os.Getenv(envKey); val != "" {
			if parsed, err := parseBool(val); err == nil {
				*target = parsed
			}
		}
	}
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "t", "yes", "on":
		return true, nil
	case "0", "false", "f", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool: %q", s)
	}
}
