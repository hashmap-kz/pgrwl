package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"text/template"

	"github.com/hashmap-kz/pgrwl/internal/opt/optutils"

	"github.com/hashmap-kz/pgrwl/cmd"
	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/urfave/cli/v3"
)

func main() {
	var cfg *config.Config
	setupUsage()

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
			if configPath == "" {
				log.Fatal("config path is not defined")
			}
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
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func setupUsage() {
	customHelpTemplate := `
NAME:
   {{.Name}} - {{.Usage}}

USAGE:
  # RECEIVE/SERVE MODE (requires appropriate sections in config.json)
  pgrwl -c config.json
  
  # basic config for 'receive' mode:
  {
    "mode": {
      "name": "receive",
      "receive": {
        "listen_port": 7070,
        "directory": "wals",
        "slot": "pgrwl_v5"
      }
    }
  }

  # basic config fot 'serve' mode:
  {
    "mode": {
      "name": "serve",
      "serve": {
        "listen_port": 7070,
        "directory": "wals"
      }
    }
  }

  # RESTORE MODE (example usage in postgresql.conf):
  restore_command = 'pgrwl restore-command --serve-addr=k8s-worker5:30266 %f %p'

GLOBAL OPTIONS:
{{range .VisibleFlags}}{{.}}
{{end}}
`

	cli.HelpPrinter = func(w io.Writer, templ string, data any) {
		t := template.Must(template.New("help").Parse(customHelpTemplate))
		if err := t.Execute(w, data); err != nil {
			_, _ = fmt.Fprintln(w, "failed to render help:", err)
		}
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
