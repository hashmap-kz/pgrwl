package main

import (
	"context"
	"log"
	"os"

	"github.com/hashmap-kz/pgrwl/cmd"
)

func main() {
	app := cmd.App()
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
