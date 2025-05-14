package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/cmd"
)

func main() {
	configPath := flag.String("c", "", "config file path")
	flag.Parse()
	_ = config.Read(*configPath)

	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
