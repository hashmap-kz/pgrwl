package loops

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

type ReceiveModeOpts struct {
	Directory  string
	Slot       string
	NoLoop     bool
	ListenPort int
	Verbose    bool
}

func RunStreamingLoop(ctx context.Context, pgrw *xlog.PgReceiveWal, opts *ReceiveModeOpts) error {
	// enter main streaming loop
	for {
		err := pgrw.StreamLog(ctx)
		if err != nil {
			slog.Error("an error occurred in StreamLog(), exiting",
				slog.Any("err", err),
			)
			os.Exit(1)
		}

		select {
		case <-ctx.Done():
			slog.Info("(main) received termination signal, exiting...")
			os.Exit(0)
		default:
		}

		if opts.NoLoop {
			slog.Error("disconnected")
			os.Exit(1)
		}

		slog.Info("disconnected; waiting 5 seconds to try again")
		time.Sleep(5 * time.Second)
	}
}
