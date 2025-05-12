package cmd

import (
	"fmt"
	"os"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"
	"github.com/spf13/cobra"
)

var rootOpts struct {
	LogLevel        string
	LogFormat       string
	LogAddSource    bool
	HTTPServerAddr  string
	HTTPServerToken string
}

var rootCmd = &cobra.Command{
	Use:          "pgrwl",
	Short:        "Stream and manage write-ahead logs from a PostgreSQL server",
	SilenceUsage: false,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()

		applyStringFallback(f, "log-level", &rootOpts.LogLevel, "PGRWL_LOG_LEVEL")
		applyStringFallback(f, "log-format", &rootOpts.LogFormat, "PGRWL_LOG_FORMAT")
		applyBoolFallback(f, "log-add-source", &rootOpts.LogAddSource, "PGRWL_LOG_ADD_SOURCE")

		// Set envs for consistency
		_ = os.Setenv("LOG_LEVEL", rootOpts.LogLevel)
		_ = os.Setenv("LOG_FORMAT", rootOpts.LogFormat)
		if rootOpts.LogAddSource {
			_ = os.Setenv("LOG_ADD_SOURCE", "1")
		}

		// check required
		applyStringFallback(f, "http-server-addr", &rootOpts.HTTPServerAddr, "PGRWL_HTTP_SERVER_ADDR")
		applyStringFallback(f, "http-server-token", &rootOpts.HTTPServerToken, "PGRWL_HTTP_SERVER_TOKEN")
		if rootOpts.HTTPServerAddr == "" {
			return fmt.Errorf("missing required flag: --http-server-addr or $PGRWL_HTTP_SERVER_ADDR")
		}
		if rootOpts.HTTPServerToken == "" {
			return fmt.Errorf("missing required flag: --http-server-token or $PGRWL_HTTP_SERVER_TOKEN")
		}
		_ = os.Setenv("PGRWL_HTTP_SERVER_TOKEN", rootOpts.HTTPServerToken)

		// Initialize logger
		logger.Init()
		return nil
	},
}

func init() {
	// Logging
	rootCmd.PersistentFlags().StringVar(&rootOpts.LogLevel, "log-level", "", "Log level (ENV: PGRWL_LOG_LEVEL)")
	rootCmd.PersistentFlags().StringVar(&rootOpts.LogFormat, "log-format", "", "Log format (ENV: PGRWL_LOG_FORMAT)")
	rootCmd.PersistentFlags().BoolVar(&rootOpts.LogAddSource, "log-add-source", false, "Include source info in logs (ENV: PGRWL_LOG_ADD_SOURCE)")
	rootCmd.PersistentFlags().StringVar(&rootOpts.HTTPServerAddr, "http-server-addr", "127.0.0.1:5080", "Run HTTP server (ENV: PGRWL_HTTP_SERVER_ADDR)")
	rootCmd.PersistentFlags().StringVar(&rootOpts.HTTPServerToken, "http-server-token", "pgrwladmin", "HTTP server token (ENV: PGRWL_HTTP_SERVER_TOKEN)")
}

// Execute main, entry point
func Execute() error {
	return rootCmd.Execute()
}
