package receivemode

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

type StreamController struct {
	mu      sync.Mutex
	running bool

	parentCtx context.Context
	log       *slog.Logger

	pgrw   xlog.PgReceiveWal
	cancel context.CancelFunc
}

func NewStreamController(parentCtx context.Context, log *slog.Logger, pgrw xlog.PgReceiveWal) *StreamController {
	return &StreamController{
		parentCtx: parentCtx,
		log:       log.With("component", "stream-controller"),
		pgrw:      pgrw,
	}
}

func (c *StreamController) Start(wg *sync.WaitGroup) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		c.log.Info("stream already running")
		return
	}

	streamCtx, cancel := context.WithCancel(c.parentCtx)
	c.cancel = cancel
	c.running = true

	wg.Add(1)
	go func() {
		defer wg.Done()

		c.log.Info("wal-receiver starting")
		err := c.pgrw.Run(streamCtx)

		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			c.log.Error("wal-receiver stopped with error", slog.Any("err", err))
			// decide here if you want to cancel whole app on *fatal* errors
			// c.parentCancel() // -> to keep base behaviour
		} else {
			c.log.Info("wal-receiver stopped")
		}
	}()
}

func (c *StreamController) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.cancel == nil {
		c.log.Info("stream already stopped")
		return
	}
	c.log.Info("stopping wal-receiver")
	c.cancel()
}

func (c *StreamController) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}
