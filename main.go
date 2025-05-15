package main

import (
	"log"

	"github.com/hashmap-kz/pgrwl/cmd"
)

func main() {
	cfg := cmd.ReadConfig()
	if cfg.Mode == cmd.ModeReceive {
		cmd.RunReceiveMode(&cmd.ReceiveModeOpts{
			Directory:  cfg.Directory,
			Slot:       cfg.ReceiveSlot,
			NoLoop:     cfg.ReceiveNoLoop,
			ListenPort: cfg.ReceiveListenPort,
		})
	} else if cfg.Mode == cmd.ModeRestore {
		cmd.RunRestoreMode(&cmd.RestoreModeOpts{
			Directory:  cfg.Directory,
			ListenPort: cfg.RestoreListenPort,
		})
	} else {
		log.Fatalf("unknown mode: %s", cfg.Mode)
	}
}
