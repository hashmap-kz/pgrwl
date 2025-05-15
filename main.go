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

const (
	flagDirectory    = "directory"
	flagListenPort   = "listen-port"
	flagSlot         = "slot"
	flagNoLoop       = "no-loop"
	flagLogLevel     = "log-level"
	flagLogFormat    = "log-format"
	flagLogAddSource = "log-add-source"
)

func init() {
	_ = os.Setenv("PGRWL_LISTEN_PORT", "7070")
	_ = os.Setenv("PGRWL_STORAGE_TYPE", "local")
}

func main() {
	var cfg *config.Config

	dirFlag := &cli.StringFlag{
		Name:     flagDirectory,
		Usage:    "WAL-archive directory",
		Aliases:  []string{"D"},
		Required: true,
	}
	listenPortFlag := &cli.IntFlag{
		Name:     flagListenPort,
		Usage:    "HTTP port",
		Aliases:  []string{"p"},
		Required: true,
		Sources:  cli.EnvVars("PGRWL_LISTEN_PORT"),
	}

	app := &cli.Command{
		Name:  "pgrwl",
		Usage: "PostgreSQL WAL receiver and restore tool",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Usage:   "Path to config file (*.json)",
				Aliases: []string{"c"},
				Sources: cli.EnvVars("PGRWL_CONFIG_PATH"),
			},
			&cli.StringFlag{
				Name:    flagLogLevel,
				Value:   "info",
				Usage:   "Log level (e.g. trace, debug, info, warn, error)",
				Sources: cli.EnvVars("PGRWL_LOG_LEVEL"),
			},
			&cli.StringFlag{
				Name:    flagLogFormat,
				Value:   "json",
				Usage:   "Log format (e.g. text, json)",
				Sources: cli.EnvVars("PGRWL_LOG_FORMAT"),
			},
			&cli.BoolFlag{
				Name:    flagLogAddSource,
				Usage:   "Enable source field in logs",
				Sources: cli.EnvVars("PGRWL_LOG_ADD_SOURCE"),
			},
		},
		Before: func(ctx context.Context, c *cli.Command) (context.Context, error) {
			configPath := c.String("config")
			if configPath != "" {
				cfg = config.MustRead(configPath)
			} else {
				cfg = config.Default()
			}
			_, _ = fmt.Fprintln(os.Stderr, cfg.String()) // debug config (NOTE: sensitive fields are hidden)
			logger.Init(&logger.Opts{
				Level:     c.String(flagLogLevel),
				Format:    c.String(flagLogFormat),
				AddSource: c.Bool(flagLogAddSource),
			})
			return nil, nil
		},
		Commands: []*cli.Command{
			{
				Name:  "receive",
				Usage: "Stream and archive WALs",
				Flags: []cli.Flag{
					dirFlag,
					listenPortFlag,
					&cli.StringFlag{
						Name:    flagSlot,
						Aliases: []string{"S"},
						Usage:   "Replication slot to use",
						Value:   "pgrwl_v5",
						Sources: cli.EnvVars("PGRWL_RECEIVE_SLOT"),
					},
					&cli.BoolFlag{
						Name:    flagNoLoop,
						Aliases: []string{"n"},
						Usage:   "Do not loop on connection lost",
						Sources: cli.EnvVars("PGRWL_RECEIVE_NO_LOOP"),
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					checkPgEnvsAreSet()
					cmd.RunReceiveMode(&cmd.ReceiveModeOpts{
						Directory:  c.String(flagDirectory),
						ListenPort: c.Int(flagListenPort),
						Slot:       c.String(flagSlot),
						NoLoop:     c.Bool(flagNoLoop),
						Verbose:    strings.EqualFold(c.String(flagLogLevel), "trace"),
					})
					return nil
				},
			},
			{
				Name:  "serve",
				Usage: "Serve WAL files for restore",
				Flags: []cli.Flag{
					dirFlag,
					listenPortFlag,
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					cmd.RunServeMode(&cmd.ServeModeOpts{
						Directory:  c.String(flagDirectory),
						ListenPort: c.Int(flagListenPort),
					})
					return nil
				},
			},
			{
				Name:  "restore-command",
				Usage: "Fetch a single WAL file by name",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "addr",
						Required: true,
						Usage:    "The address of pgrwl running in a serve mode",
					},
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

func checkPgEnvsAreSet() {
	// TODO: PGPASSFILE, etc...
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
