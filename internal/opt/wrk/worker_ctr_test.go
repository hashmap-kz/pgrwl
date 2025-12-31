package wrk

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// newTestLogger creates a slog.Logger that discards output.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// Test that a new controller starts in "stopped" state.
func TestWorkerController_InitialStatus(t *testing.T) {
	ctx := context.Background()
	log := newTestLogger(t)

	ctrl := NewWorkerController(ctx, log, func(_ context.Context) error {
		return nil
	})

	assert.Equal(t, "stopped", ctrl.Status())
}

// Test that Start actually runs the worker and Status reflects "running".
func TestWorkerController_StartAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := newTestLogger(t)

	started := make(chan struct{})
	runCtxCanceled := make(chan struct{})

	runFn := func(ctx context.Context) error {
		// Signal that worker goroutine has started.
		close(started)

		// Wait until context is canceled.
		<-ctx.Done()
		close(runCtxCanceled)
		return ctx.Err()
	}

	ctrl := NewWorkerController(ctx, log, runFn)

	// Initially stopped.
	assert.Equal(t, "stopped", ctrl.Status())

	// Start worker.
	ctrl.Start()

	// Wait for runFn to signal start.
	select {
	case <-started:
		// ok
	case <-time.After(time.Second):
		assert.Fail(t, "worker did not start in time")
	}

	// Should now be running.
	assert.Equal(t, "running", ctrl.Status())

	// Stop worker.
	ctrl.Stop()

	// Wait until runFn sees context cancellation.
	select {
	case <-runCtxCanceled:
		// ok
	case <-time.After(time.Second):
		assert.Fail(t, "worker did not observe ctx cancellation in time")
	}

	// Wait should return after worker finishes.
	done := make(chan struct{})
	go func() {
		ctrl.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		assert.Fail(t, "Wait did not return in time")
	}

	// After stop, with parent still alive, status should be "stopped".
	assert.Equal(t, "stopped", ctrl.Status())
}

// Test that Start is idempotent when already running (runFn called once).
func TestWorkerController_Start_Idempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := newTestLogger(t)

	var runCount int32
	started := make(chan struct{})

	runFn := func(ctx context.Context) error {
		if atomic.AddInt32(&runCount, 1) == 1 {
			// First (and only) run: signal started.
			close(started)
		}
		<-ctx.Done()
		return ctx.Err()
	}

	ctrl := NewWorkerController(ctx, log, runFn)

	ctrl.Start()

	// Wait for first run to start.
	select {
	case <-started:
	case <-time.After(time.Second):
		assert.Fail(t, "worker did not start in time")
	}

	// Call Start again while already running.
	ctrl.Start()

	// Stop and wait.
	ctrl.Stop()
	ctrl.Wait()

	// Verify runFn was only invoked once.
	assert.Equal(t, int32(1), atomic.LoadInt32(&runCount), "runFn should be called exactly once")
}

// Test that Start does nothing if parent context is already canceled.
func TestWorkerController_StartWithCanceledParent(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	parentCancel() // cancel before controller is created

	log := newTestLogger(t)

	var runCount int32
	runFn := func(_ context.Context) error {
		atomic.AddInt32(&runCount, 1)
		return nil
	}

	ctrl := NewWorkerController(parentCtx, log, runFn)

	// Parent already canceled; Start should not run worker.
	ctrl.Start()
	ctrl.Wait()

	assert.Equal(t, int32(0), atomic.LoadInt32(&runCount), "runFn should not be called when parent is canceled")
	assert.Equal(t, "stopped(parent-canceled)", ctrl.Status())
}

// Test that Stop on a non-running worker is safe and keeps status "stopped".
func TestWorkerController_StopWhenNotRunning(t *testing.T) {
	ctx := context.Background()
	log := newTestLogger(t)

	ctrl := NewWorkerController(ctx, log, func(_ context.Context) error {
		return nil
	})

	// No worker was started.
	assert.Equal(t, "stopped", ctrl.Status())

	// Calling Stop should be a no-op and not panic.
	ctrl.Stop()
	ctrl.Wait() // should return immediately

	assert.Equal(t, "stopped", ctrl.Status())
}

// Test that Wait returns even if Start was never called.
func TestWorkerController_WaitWithoutStart(t *testing.T) {
	ctx := context.Background()
	log := newTestLogger(t)

	ctrl := NewWorkerController(ctx, log, func(_ context.Context) error {
		return nil
	})

	done := make(chan struct{})
	go func() {
		ctrl.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok: Wait returned immediately because wg counter is zero.
	case <-time.After(time.Second):
		assert.Fail(t, "Wait blocked even though worker was never started")
	}
}
