package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/strx"

	"github.com/hashmap-kz/pgrwl/internal/version"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/urfave/cli/v3"
)

func App() *cli.Command {
	configFlag := &cli.StringFlag{
		Name:    "config",
		Usage:   "Path to config file",
		Aliases: []string{"c"},
		Sources: cli.EnvVars("PGRWL_CONFIG_PATH"),
	}
	modeFlag := &cli.StringFlag{
		Name:     "mode",
		Usage:    "Daemon mode: receive/serve/backup",
		Aliases:  []string{"m"},
		Required: true,
		Sources:  cli.EnvVars("PGRWL_DAEMON_MODE"),
	}

	app := &cli.Command{
		Name:    "pgrwl",
		Usage:   "Cloud-Native PostgreSQL WAL receiver",
		Version: version.Version,
		Commands: []*cli.Command{
			// server modes
			{
				Name:  "daemon",
				Usage: "Running in a daemon mode: receive/serve/backup",
				Flags: []cli.Flag{
					configFlag,
					modeFlag,
				},
				Action: func(_ context.Context, c *cli.Command) error {
					mode := c.String("mode")
					cfg := loadConfig(c, mode)
					verbose := strings.EqualFold(cfg.Log.Level, "trace")

					//nolint:staticcheck
					if mode == config.ModeReceive {
						checkPgEnvsAreSet()
						RunReceiveMode(&ReceiveModeOpts{
							ReceiveDirectory: filepath.ToSlash(cfg.Main.Directory),
							ListenPort:       cfg.Main.ListenPort,
							Slot:             cfg.Receiver.Slot,
							NoLoop:           cfg.Receiver.NoLoop,
							Verbose:          verbose,
						})
					} else if mode == config.ModeBackup {
						checkPgEnvsAreSet()
						RunBackupMode(&BackupModeOpts{
							ReceiveDirectory: filepath.ToSlash(cfg.Main.Directory),
							Verbose:          verbose,
						})
					} else {
						log.Fatalf("unknown mode: %s", mode)
					}

					return nil
				},
			},

			// basebackup create
			{
				Name:  "backup",
				Usage: "Create basebackup using streaming replication protocol",
				Flags: []cli.Flag{
					configFlag,
				},
				Action: func(_ context.Context, c *cli.Command) error {
					checkPgEnvsAreSet()
					cfg := loadConfig(c, config.ModeBackupCMD)
					err := RunBaseBackup(&BaseBackupCmdOpts{Directory: cfg.Main.Directory})
					return err
				},
			},

			// basebackup restore
			{
				Name:  "restore",
				Usage: "Retrieve basebackup",
				Flags: []cli.Flag{
					configFlag,
					&cli.StringFlag{
						Name:  "id",
						Usage: "Backup id to restore (20060102150405), the 'latest' will be used if not set",
					},
					&cli.StringFlag{
						Name:     "dest",
						Usage:    "Restore to destination",
						Required: true,
					},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					cfg := loadConfig(c, config.ModeRestoreCMD)
					err := RestoreBaseBackup(context.Background(), cfg,
						c.String("id"),
						c.String("dest"),
					)
					return err
				},
			},

			// restore-command
			{
				Name:  "restore-command",
				Usage: "Fetch a single WAL file by name",

				Description: strx.HeredocTrim(`
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

					return ExecRestoreCommand(
						walFile,
						destPath,
						&RestoreCommandOpts{
							Addr: c.String("serve-addr"),
						},
					)
				},
			},
			// Validate command
			{
				Name:  "validate",
				Usage: "Validate the config file without running the application",
				Flags: []cli.Flag{
					configFlag,
					modeFlag,
				},
				Action: func(_ context.Context, c *cli.Command) error {
					mode := c.String("mode")
					if mode == "" {
						log.Fatal("required flag 'mode' is empty")
					}
					_ = loadConfig(c, mode)
					fmt.Println("Configuration is valid.")
					return nil
				},
			},
		},
	}

	return app
}

func loadConfig(c *cli.Command, mode string) *config.Config {
	configPath := c.String("config")

	// 1) if -c flag is set -> must read config from file
	// 2) if $PGRWL_CONFIG_PATH is set -> must read config from file
	// 3) read config with go-envconfig otherwise
	var cfg *config.Config
	if configPath != "" {
		cfg = config.MustLoad(configPath, mode)
	} else {
		cfg = config.MustEnvconfig(mode)
	}

	// debug config (NOTE: sensitive fields are hidden)
	_, _ = fmt.Fprintf(os.Stderr, "STARTING WITH CONFIGURATION (%s):\n%s\n\n",
		filepath.ToSlash(configPath),
		cfg.String(),
	)

	logger.Init(&logger.Opts{
		Level:     cfg.Log.Level,
		Format:    cfg.Log.Format,
		AddSource: cfg.Log.AddSource,
	})
	return cfg
}

func checkPgEnvsAreSet() {
	var emptyEnvs []string
	for _, name := range []string{"PGHOST", "PGPORT", "PGUSER"} { // you can add additional fields if needed
		if os.Getenv(name) == "" {
			emptyEnvs = append(emptyEnvs, name)
		}
	}

	if os.Getenv("PGPASSWORD") == "" && os.Getenv("PGPASSFILE") == "" {
		emptyEnvs = append(emptyEnvs, "PGPASSWORD or PGPASSFILE")
	}

	if len(emptyEnvs) > 0 {
		log.Fatalf("[FATAL] receive: required env vars are empty: [%s]", strings.Join(emptyEnvs, " "))
	}

	if os.Getenv("PGPASSFILE") != "" {
		if _, err := os.Stat(os.Getenv("PGPASSFILE")); os.IsNotExist(err) {
			log.Fatalf("[FATAL] PGPASSFILE does not exist: %s", os.Getenv("PGPASSFILE"))
		}
	}
}
