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
	cfg := config.Read(*configPath)
	fmt.Println(cfg.String())

	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
