package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/pgrwl/pgrwl/ui"
	"sigs.k8s.io/yaml"
)

const defaultListenAddr = ":8080"

// Config for UI
//
// listen_addr: ":8080"
//
// receivers:
//
//   - label: localhost
//     addr: http://127.0.0.1:7070
//
//   - label: prod-db-01
//     addr: http://10.0.0.11:9090
type Config struct {
	ListenAddr string        `json:"listen_addr" yaml:"listen_addr"`
	Receivers  []ui.Receiver `json:"receivers" yaml:"receivers"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := loadConfig(configPath())
	if err != nil {
		logger.Error("load config failed", slog.Any("err", err))
		os.Exit(1)
	}

	server := ui.NewServer(ui.Options{
		Logger:    logger,
		Receivers: cfg.Receivers,
	})

	mux := http.NewServeMux()
	server.Mount(mux)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("serving pgrwl ui", slog.String("addr", cfg.ListenAddr))

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func configPath() string {
	if v := os.Getenv("PGRWL_UI_CONFIG"); v != "" {
		return v
	}
	return "pgrwl-ui.yaml"
}

func loadConfig(path string) (Config, error) {
	cfg := Config{
		ListenAddr: defaultListenAddr,
		Receivers: []ui.Receiver{
			{Label: "localhost", Addr: "http://127.0.0.1:7070"},
		},
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, err
	}

	switch {
	case hasExt(path, ".json"):
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse json config %q: %w", path, err)
		}
	default:
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse yaml config %q: %w", path, err)
		}
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.ListenAddr == "" {
		return errors.New("listen_addr is required")
	}

	if len(cfg.Receivers) == 0 {
		return errors.New("at least one receiver is required")
	}

	seen := make(map[string]struct{}, len(cfg.Receivers))

	for i, r := range cfg.Receivers {
		if r.Label == "" {
			return fmt.Errorf("receivers[%d].label is required", i)
		}
		if r.Addr == "" {
			return fmt.Errorf("receivers[%d].addr is required", i)
		}
		if _, ok := seen[r.Label]; ok {
			return fmt.Errorf("duplicate receiver label %q", r.Label)
		}
		seen[r.Label] = struct{}{}
	}

	return nil
}

func hasExt(path, ext string) bool {
	return len(path) >= len(ext) && path[len(path)-len(ext):] == ext
}
