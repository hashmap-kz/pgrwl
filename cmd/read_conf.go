package cmd

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
)

const (
	ModeReceive = "receive"
	ModeRestore = "restore"
)

func ReadConfig() *config.Config {
	configPath := flag.String("c", "", "config file path")
	flag.Parse()
	cfg := config.Read(*configPath)

	// check required
	if cfg.Mode == "" {
		log.Fatal("mode is not defined")
	}
	if cfg.Directory == "" {
		log.Fatal("directory is not defined")
	}

	if cfg.Mode == ModeReceive {
		if cfg.ReceiveListenPort == 0 {
			log.Fatal("receive. listen-port is not defined")
		}
		if cfg.ReceiveSlot == "" {
			log.Fatal("receive. slot is not defined")
		}

		// Validate required PG env vars
		var emptyEnvs []string
		for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD"} {
			if os.Getenv(name) == "" {
				emptyEnvs = append(emptyEnvs, name)
			}
		}
		if len(emptyEnvs) > 0 {
			log.Fatalf("required env vars are empty: [%s]", strings.Join(emptyEnvs, " "))
		}
	} else if cfg.Mode == ModeRestore {
		if cfg.RestoreListenPort == 0 {
			log.Fatal("restore. listen-port is not defined")
		}
		if cfg.RestoreFetchAddr == "" {
			log.Fatal("restore. addr is not defined")
		}
	} else {
		log.Fatalf("unexpected mode: %s", cfg.Mode)
	}

	// init logger
	logger.Init(&logger.Opts{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: cfg.LogAddSource,
	})

	return cfg
}
