package cmd

import (
	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/spf13/cobra"
)

var rootOpts struct {
	LogLevel     string
	LogFormat    string
	LogAddSource bool
}

var rootCmd = &cobra.Command{
	Use:           "pgrwl",
	Short:         "Stream and manage write-ahead logs from a PostgreSQL server",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()

		applyStringFallback(f, "log-level", &rootOpts.LogLevel, "PGRWL_LOG_LEVEL")
		applyStringFallback(f, "log-format", &rootOpts.LogFormat, "PGRWL_LOG_FORMAT")
		applyBoolFallback(f, "log-add-source", &rootOpts.LogAddSource, "PGRWL_LOG_ADD_SOURCE")

		// Initialize logger
		logger.Init(&logger.Opts{
			Level:     rootOpts.LogLevel,
			Format:    rootOpts.LogFormat,
			AddSource: rootOpts.LogAddSource,
		})
		return nil
	},
}

func init() {
	// Logging
	rootCmd.PersistentFlags().StringVar(&rootOpts.LogLevel, "log-level", "info", "Log level: trace/debug/info/warn/error (ENV: PGRWL_LOG_LEVEL)")
	rootCmd.PersistentFlags().StringVar(&rootOpts.LogFormat, "log-format", "json", "Log format: text/json (ENV: PGRWL_LOG_FORMAT)")
	rootCmd.PersistentFlags().BoolVar(&rootOpts.LogAddSource, "log-add-source", false, "Include source info in logs (ENV: PGRWL_LOG_ADD_SOURCE)")
}

// Execute main, entry point
func Execute() error {
	return rootCmd.Execute()
}
