package main

import (
	"context"
	"log"
	"os"

	"github.com/pgrwl/pgrwl/cmd/pgrwl/app"
)

func main() {
	application := app.App()
	if err := application.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
