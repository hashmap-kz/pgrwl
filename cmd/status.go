package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get WAL receiver status",
	RunE: func(cmd *cobra.Command, args []string) error {
		req, err := http.NewRequest("GET", "http://localhost:5080/status", nil)
		if err != nil {
			return err
		}

		// Set auth header
		req.Header.Set("Authorization", "Bearer "+os.Getenv("PGRWL_HTTP_SERVER_TOKEN"))

		// Optional: Set content type or other headers
		// req.Header.Set("Accept", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			return err
		}

		fmt.Println("Status:")
		for k, v := range status {
			fmt.Printf("  %s: %v\n", k, v)
		}

		return nil
	},
}
