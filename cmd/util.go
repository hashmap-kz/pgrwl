package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
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

func runHTTPServer(ctx context.Context, port int, router http.Handler) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
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
