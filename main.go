package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"pgreceivewal5/internal/xlog"
)

// utils+
const (
	walDir = "wals"
)

func populateTestDir() {
	files := []string{
		"00000001000000000000004C",
		"00000001000000000000004D.partial",
	}
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		log.Fatal(err)
	}
	for _, fname := range files {
		file, err := os.Create(filepath.Join(walDir, fname))
		if err != nil {
			log.Fatalf("failed to create file: %v", err)
		}
		defer file.Close()

		if err := file.Truncate(xlog.WalSegSz); err != nil {
			log.Fatalf("failed to set file size: %v", err)
		}
	}
}

// utils-

func main() {
	populateTestDir()

	startLSN, timeline, err := xlog.FindStreamingStart("wals")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(startLSN, timeline)
}
