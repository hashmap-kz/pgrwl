package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hashmap-kz/pgrwl/cmd"
	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/urfave/cli/v3"
)

func main() {
	var cfg *config.Config

	configFlag := &cli.StringFlag{
		Name:     "config",
		Usage:    "Path to config.json",
		Aliases:  []string{"c"},
		Required: true,
	}

	app := &cli.Command{
		Name:  "pgrwl",
		Usage: "PostgreSQL WAL receiver and restore tool",
		Flags: []cli.Flag{
			configFlag,
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			cfg = config.Read(c.String("config"))
			_, _ = fmt.Fprintln(os.Stderr, cfg.String()) // debug config (NOTE: sensitive fields are hidden)
			logger.Init(&logger.Opts{
				Level:     cfg.LogLevel,
				Format:    cfg.LogFormat,
				AddSource: cfg.LogAddSource,
			})
			return nil, nil
		},
		Commands: []*cli.Command{
			{
				Name:  "receive",
				Usage: "Stream and archive WALs",
				Action: func(ctx context.Context, c *cli.Command) error {
					checkReceiveCfg(cfg)
					cmd.RunReceiveMode(&cmd.ReceiveModeOpts{
						Directory:  cfg.Directory,
						Slot:       cfg.ReceiveSlot,
						NoLoop:     cfg.ReceiveNoLoop,
						ListenPort: cfg.ReceiveListenPort,
					})
					return nil
				},
			},
			{
				Name:  "serve",
				Usage: "Serve WAL files for restore",
				Action: func(ctx context.Context, c *cli.Command) error {
					checkServeCfg(cfg)
					cmd.RunServeMode(&cmd.ServeModeOpts{
						Directory:  cfg.Directory,
						ListenPort: cfg.ServeListenPort,
					})
					return nil
				},
			},
			{
				Name:  "restore-command",
				Usage: "Fetch a single WAL file by name",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "f",
						Required: true,
						Usage:    "WAL file name to fetch",
					},
					&cli.StringFlag{
						Name:     "p",
						Required: true,
						Usage:    "Output path for the fetched WAL",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func checkServeCfg(cfg *config.Config) {
	if cfg.Directory == "" {
		log.Fatal("[FATAL] serve: directory is not defined")
	}
	if cfg.ServeListenPort == 0 {
		log.Fatal("[FATAL] serve: listen-port is not defined")
	}
}

func checkReceiveCfg(cfg *config.Config) {
	if cfg.Directory == "" {
		log.Fatal("[FATAL] receive: directory is not defined")
	}
	if cfg.ReceiveListenPort == 0 {
		log.Fatal("[FATAL] receive: listen-port is not defined")
	}
	if cfg.ReceiveSlot == "" {
		log.Fatal("[FATAL] receive: slot is not defined")
	}

	// Validate required PG env vars
	var emptyEnvs []string
	for _, name := range []string{"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD"} {
		if os.Getenv(name) == "" {
			emptyEnvs = append(emptyEnvs, name)
		}
	}
	if len(emptyEnvs) > 0 {
		log.Fatalf("[FATAL] receive: required env vars are empty: [%s]", strings.Join(emptyEnvs, " "))
	}
}
