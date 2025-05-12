package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"os"
)

var getWalOpts struct {
	HTTPServerAddr  string
	HTTPServerToken string
}

func init() {
	rootCmd.AddCommand(getWalCmd)
	getWalCmd.Flags().StringVar(&getWalOpts.HTTPServerAddr, "http-server-addr", "", "Run HTTP server (ENV: PGRWL_HTTP_SERVER_ADDR)")
	getWalCmd.Flags().StringVar(&getWalOpts.HTTPServerToken, "http-server-token", "", "HTTP server token (ENV: PGRWL_HTTP_SERVER_TOKEN)")
}

// restore_command = 'pgrwl wal-restore %f %p'
var getWalCmd = &cobra.Command{
	Use:   "wal-restore",
	Short: "Download a WAL file from the running receiver",
	Long: `
Implements PostgreSQL restore_command, example usage in postgresql.conf:
restore_command = 'pgrwl wal-restore %f %p'
`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		walFileName := args[0]
		walFilePath := args[1]
		url := fmt.Sprintf("http://localhost:5080/wal/%s", walFileName)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		if getWalOpts.HTTPServerToken != "" {
			req.Header.Set("Authorization", "Bearer "+getWalOpts.HTTPServerToken)
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
	},
}
