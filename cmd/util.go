package cmd

import (
	"fmt"
	"net"
	"strings"
)

// HTTP

func addr(from string) (string, error) {
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
