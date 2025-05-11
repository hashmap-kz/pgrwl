package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var getWalCmd = &cobra.Command{
	Use:   "get-wal [filename]",
	Short: "Download a WAL file from the running receiver",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filename := args[0]
		token := os.Getenv("PGRWL_HTTP_SERVER_TOKEN")
		url := fmt.Sprintf("http://localhost:5080/wal/%s", filename)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
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
		out, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, resp.Body)
		if err == nil {
			fmt.Printf("Saved %s\n", filename)
		}
		return err
	},
}
