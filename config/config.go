//go:generate go run ../cmd/genmerge/main.go

package config

import (
	"encoding/json"
	"log"
	"os"
	"sync"
)

var (
	once   sync.Once
	config *Config

	DefaultValues = map[string]string{
		"PGRWL_MODE":        "receive",
		"PGRWL_SLOT":        "pgrwl_v5",
		"PGRWL_LISTEN_PORT": "7070",
		"PGRWL_LOG_LEVEL":   "info",
		"PGRWL_LOG_FORMAT":  "json",
	}
)

type Config struct {
	Mode         string `json:"PGRWL_MODE" envDefault:"receive"`
	Directory    string `json:"PGRWL_DIRECTORY"`
	Slot         string `json:"PGRWL_SLOT" envDefault:"pgrwl_v5"`
	NoLoop       bool   `json:"PGRWL_NO_LOOP"`
	ListenPort   int    `json:"PGRWL_LISTEN_PORT" envDefault:"7070"`
	LogLevel     string `json:"PGRWL_LOG_LEVEL" envDefault:"info"`
	LogFormat    string `json:"PGRWL_LOG_FORMAT" envDefault:"json"`
	LogAddSource bool   `json:"PGRWL_LOG_ADD_SOURCE"`

	// S3 Storage config
	S3URL             string `json:"PGRWL_S3_URL"`
	S3AccessKeyID     string `json:"PGRWL_S3_ACCESS_KEY_ID"`
	S3SecretAccessKey string `json:"PGRWL_S3_SECRET_ACCESS_KEY"`
	S3Bucket          string `json:"PGRWL_S3_BUCKET"`
	S3Region          string `json:"PGRWL_S3_REGION"`
	S3UsePathStyle    bool   `json:"PGRWL_S3_USE_PATH_STYLE"`
	S3DisableSSL      bool   `json:"PGRWL_S3_DISABLE_SSL"`
}

func Cfg() *Config {
	if config == nil {
		log.Fatal("config was not loaded in main")
	}
	return config
}

func Read(path string) *Config {
	once.Do(func() {
		var err error
		config, err = loadCfg(path, DefaultValues)
		if err != nil {
			log.Fatal(err)
		}
	})
	return config
}

func loadCfg(path string, defaults map[string]string) (*Config, error) {
	var c Config
	// if path is set, must read from file
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
	}
	// fill from env if JSON did not provide values
	mergeEnvIfUnset(&c, defaults)
	return &c, nil
}
