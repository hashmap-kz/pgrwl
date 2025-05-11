package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "pgrwl",
	Short: "Stream and manage write-ahead logs from a PostgreSQL server",
	// TODO:
	Long:         ``,
	SilenceUsage: true,
}

// Execute main, entry point
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(receiveCmd)
}
