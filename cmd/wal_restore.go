package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(walRestoreCmd)
}

// restore_command = 'pgrwl wal-restore %f %p'
var walRestoreCmd = &cobra.Command{
	Use:   "wal-restore",
	Short: "Download a WAL file from the server",
	Long: `
Implements PostgreSQL restore_command, example usage in postgresql.conf:
restore_command = 'pgrwl wal-restore %f %p'
`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		walFileName := args[0]
		walFilePath := args[1]
		return runWalRestore(walFileName, walFilePath)
	},
}

func runWalRestore(walFileName, walFilePath string) error {
	slog.Debug("wal-restore",
		slog.String("f", walFileName),
		slog.String("p", walFilePath),
	)

	addr, err := addr(rootOpts.HTTPServerAddr)
	if err != nil {
		return err
	}
	baseURL := fmt.Sprintf("%s/wal/%s", addr, walFileName)

	req, err := http.NewRequest("GET", baseURL, nil)
	if err != nil {
		return err
	}
	if rootOpts.HTTPServerToken != "" {
		req.Header.Set("Authorization", "Bearer "+rootOpts.HTTPServerToken)
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error: %s", resp.Status)
	}

	// Save to file
	fileDst, err := os.OpenFile(walFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	defer fileDst.Close()

	_, err = io.Copy(fileDst, resp.Body)
	return err
}
