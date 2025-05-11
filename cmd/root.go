package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use: "pgrwl",
	// TODO:
	Short:        "",
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
