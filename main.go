package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hashmap-kz/pgrwl/cmd/loops"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"

	"github.com/hashmap-kz/pgrwl/cmd"
	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/urfave/cli/v3"
)

func main() {
	configFlag := &cli.StringFlag{
		Name:     "config",
		Usage:    "Path to config file (*.json)",
		Aliases:  []string{"c"},
		Required: true,
		Sources:  cli.EnvVars("PGRWL_CONFIG_PATH"),
	}

	app := &cli.Command{
		Name:  "pgrwl",
		Usage: "PostgreSQL WAL receiver and restore tool",
		Commands: []*cli.Command{
			// server modes
			{
				Name:  "start",
				Usage: "Running in a server mode: receive/serve",
				Flags: []cli.Flag{
					configFlag,
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg := loadConfig(c)

					//nolint:staticcheck
					if cfg.Mode.Name == config.ModeReceive {
						checkPgEnvsAreSet()
						cmd.RunReceiveMode(&loops.ReceiveModeOpts{
							Directory:  cfg.Mode.Receive.Directory,
							ListenPort: cfg.Mode.Receive.ListenPort,
							Slot:       cfg.Mode.Receive.Slot,
							NoLoop:     cfg.Mode.Receive.NoLoop,
							Verbose:    strings.EqualFold(cfg.Log.Level, "trace"),
						})
					} else if cfg.Mode.Name == config.ModeServe {
						cmd.RunServeMode(&cmd.ServeModeOpts{
							Directory:  cfg.Mode.Serve.Directory,
							ListenPort: cfg.Mode.Serve.ListenPort,
							Verbose:    strings.EqualFold(cfg.Log.Level, "trace"),
						})
					} else {
						log.Fatalf("unknown mode: %s", cfg.Mode.Name)
					}

					return nil
				},
			},

			// restore-command
			{
				Name:  "restore-command",
				Usage: "Fetch a single WAL file by name",

				Description: optutils.HeredocTrim(`
				Implements PostgreSQL restore_command.

				Example usage in postgresql.conf:
				restore_command = 'pgrwl restore-command --serve-addr=k8s-worker5:30266 %f %p'
				`),

				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "serve-addr",
						Required: true,
						Usage:    "The address of pgrwl running in a serve mode",
					},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					args := c.Args()
					if args.Len() != 2 {
						return fmt.Errorf("usage: restore-command <WAL_FILE_NAME> <DEST_PATH>")
					}

					walFile := args.Get(0)
					destPath := args.Get(1)

					return cmd.ExecRestoreCommand(
						walFile,
						destPath,
						&cmd.RestoreCommandOpts{
							Addr: c.String("serve-addr"),
						},
					)
				},
			},
			// config-template
			{
				Name:  "config-template",
				Usage: "Get a '*.json' file with all properties set",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "r",
						Usage: "Minimal config for 'receive' mode",
					},
					&cli.BoolFlag{
						Name:  "s",
						Usage: "Minimal config for 'serve' mode",
					},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Bool("r") {
						_, _ = fmt.Fprintln(os.Stderr, cmd.GetConfigTemplateReceive())
					} else if c.Bool("s") {
						_, _ = fmt.Fprintln(os.Stderr, cmd.GetConfigTemplateServe())
					} else {
						_, _ = fmt.Fprintln(os.Stderr, cmd.GetConfigTemplateFull())
					}
					return nil
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func loadConfig(c *cli.Command) *config.Config {
	configPath := c.String("config")
	if configPath == "" {
		log.Fatal("config path is not defined")
	}
	cfg := config.MustLoad(configPath)

	// debug config (NOTE: sensitive fields are hidden)
	_, _ = fmt.Fprintf(os.Stderr, "STARTING WITH CONFIGURATION:\n%s\n\n", cfg.String())

	logger.Init(&logger.Opts{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
	})
	return cfg
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
