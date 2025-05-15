package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"

	"github.com/hashmap-kz/pgrwl/cmd"
	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/urfave/cli/v3"
)

func main() {
	var cfg *config.Config

	app := &cli.Command{
		Name:  "pgrwl",
		Usage: "PostgreSQL WAL receiver and restore tool",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config",
				Usage:    "Path to config file (*.json)",
				Aliases:  []string{"c"},
				Required: true,
				Sources:  cli.EnvVars("PGRWL_CONFIG_PATH"),
			},
		},
		Before: func(_ context.Context, c *cli.Command) (context.Context, error) {
			configPath := c.String("config")
			cfg = config.MustLoad(configPath)
			_, _ = fmt.Fprintln(os.Stderr, cfg.String()) // debug config (NOTE: sensitive fields are hidden)
			logger.Init(&logger.Opts{
				Level:     cfg.Log.Level,
				Format:    cfg.Log.Format,
				AddSource: cfg.Log.AddSource,
			})
			return nil, nil
		},
		Action: func(_ context.Context, c *cli.Command) error {
			//nolint:staticcheck
			if cfg.Mode.Name == config.ModeReceive {
				checkPgEnvsAreSet()
				cmd.RunReceiveMode(&cmd.ReceiveModeOpts{
					Directory:  cfg.Mode.Receive.Directory,
					ListenPort: cfg.Mode.Receive.ListenPort,
					Slot:       cfg.Mode.Receive.Slot,
					NoLoop:     cfg.Mode.Receive.NoLoop,
					Verbose:    strings.EqualFold(c.String(cfg.Log.Level), "trace"),
				})
			} else if cfg.Mode.Name == config.ModeServe {
				cmd.RunServeMode(&cmd.ServeModeOpts{
					Directory:  cfg.Mode.Serve.Directory,
					ListenPort: cfg.Mode.Serve.ListenPort,
				})
			} else {
				log.Fatalf("unknown mode: %s", cfg.Mode.Name)
			}
			return nil
		},
		Commands: []*cli.Command{
			// restore-command
			{
				Name:  "restore-command",
				Usage: "Fetch a single WAL file by name",

				Description: optutils.HeredocTrim(`
				Implements PostgreSQL restore_command.

				Example usage in postgresql.conf:
				restore_command = 'pgrwl restore-command --addr=k8s-worker5:30266 -f %f -p %p'
				`),

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
				Action: func(_ context.Context, c *cli.Command) error {
					return cmd.ExecRestoreCommand(
						c.String("f"),
						c.String("p"),
						&cmd.RestoreCommandOpts{
							Addr: c.String("addr"),
						},
					)
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
