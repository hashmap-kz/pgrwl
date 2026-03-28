package main

import (
	"context"
	"log"
	"os"

	"github.com/pgrwl/pgrwl/cmd"
)

func main() {
	app := cmd.App()
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
