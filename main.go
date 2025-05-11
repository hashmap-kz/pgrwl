package main

import (
	"fmt"
	"os"

	"github.com/hashmap-kz/pgrwl/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
