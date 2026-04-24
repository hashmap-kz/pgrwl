package main

import (
	"context"
	"github.com/pgrwl/pgrwl/cmd/pgrwl/app"
	"log"
	"os"
)

func main() {
	application := app.App()
	if err := application.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
