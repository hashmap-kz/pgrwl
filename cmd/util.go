package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// CLI

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

// HTTP

func runHTTPServer(ctx context.Context, addr string, router http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		// Context was cancelled, shut down the HTTP server gracefully
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown error", slog.Any("err", err))
		} else {
			slog.Debug("HTTP server shut down")
		}
	}()

	slog.Info("starting HTTP server", slog.String("addr", srv.Addr))

	// Start the server (blocking)
	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err // real error
	}
	return nil
}
