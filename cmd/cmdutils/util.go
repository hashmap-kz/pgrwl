package cmdutils

import (
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
)

// HTTP

func Addr(from string) (string, error) {
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

// time

func ParseDurationOrDefault(d string, fallback time.Duration) time.Duration {
	duration, err := time.ParseDuration(d)
	if err == nil {
		return duration
	}
	slog.Error("cannot parse duration", slog.String("d", d), slog.Any("err", err))
	return fallback
}
