package wrk

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

type WorkerFunc func(ctx context.Context) error

type WorkerController struct {
	mu        sync.Mutex
	log       *slog.Logger
	parentCtx context.Context

	runFn   WorkerFunc
	running bool

	ctx    context.Context    // child ctx for this run
	cancel context.CancelFunc // cancels the current run
	wg     sync.WaitGroup
}

func NewWorkerController(parentCtx context.Context, log *slog.Logger, runFn WorkerFunc) *WorkerController {
	return &WorkerController{
		log:       log,
		parentCtx: parentCtx,
		runFn:     runFn,
	}
}

func (c *WorkerController) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		c.log.Info("worker already running")
		return
	}
	if c.parentCtx.Err() != nil {
		c.log.Warn("cannot start worker: parent context canceled")
		return
	}

	childCtx, cancel := context.WithCancel(c.parentCtx)
	c.ctx = childCtx
	c.cancel = cancel
	c.running = true

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		c.log.Info("worker starting")
		err := c.runFn(childCtx)

		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.ctx = nil
		c.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			c.log.Error("worker stopped with error", slog.Any("err", err))
		} else {
			c.log.Info("worker stopped")
		}
	}()
}

func (c *WorkerController) Stop() {
	c.mu.Lock()
	cancel := c.cancel
	running := c.running
	c.mu.Unlock()

	if !running {
		c.log.Info("worker already stopped")
		return
	}
	if cancel != nil {
		c.log.Info("stopping worker")
		cancel()
	}
}

// Wait block until current run completes.
func (c *WorkerController) Wait() {
	c.wg.Wait()
}

func (c *WorkerController) Status() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return "running"
	}
	if c.parentCtx.Err() != nil {
		return "stopped(parent-canceled)"
	}
	return "stopped"
}
