package xlog

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ReceiverState represents the operational state of a RestartablePgReceiver.
type ReceiverState string

const (
	ReceiverStateRunning ReceiverState = "running"
	ReceiverStateStopped ReceiverState = "stopped"
)

// pgReceiverFactory is the function signature used to create a fresh inner
// receiver. The default implementation calls NewPgReceiver; tests inject a
// fake to avoid needing a real Postgres connection.
type pgReceiverFactory func(ctx context.Context, opts *PgReceiveWalOpts) (PgReceiveWal, error)

// RestartablePgReceiver wraps PgReceiveWal and adds Start/Stop control.
// It re-creates the underlying pgReceiveWal (and its Postgres connection) on
// every Start() so the connection is always fresh after a Stop().
type RestartablePgReceiver struct {
	mu   sync.RWMutex
	l    *slog.Logger
	opts *PgReceiveWalOpts

	// parentCtx is the process-level context; cancellation here terminates
	// everything regardless of the current receiver state.
	parentCtx context.Context

	// newInner creates a fresh receiver on each Start(). Defaults to
	// NewPgReceiver. Overrideable in tests without a real Postgres connection.
	newInner pgReceiverFactory

	// inner is the currently active receiver; nil when stopped.
	inner  PgReceiveWal
	cancel context.CancelFunc
	state  ReceiverState

	// done is closed when the current Run goroutine exits.
	done chan struct{}
}

// NewRestartablePgReceiver creates a receiver that is initially stopped.
// Call Start() to begin WAL streaming.
func NewRestartablePgReceiver(parentCtx context.Context, opts *PgReceiveWalOpts) *RestartablePgReceiver {
	return &RestartablePgReceiver{
		l:         slog.With("component", "restartable-receiver"),
		opts:      opts,
		parentCtx: parentCtx,
		state:     ReceiverStateStopped,
		newInner:  NewPgReceiver,
	}
}

func (r *RestartablePgReceiver) log() *slog.Logger {
	if r.l != nil {
		return r.l
	}
	return slog.With("component", "restartable-receiver")
}

// Start begins WAL streaming. Returns an error if the receiver is already
// running or if the initial Postgres connection cannot be established.
// The caller must call Stop() before Start() can be called again.
func (r *RestartablePgReceiver) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.state == ReceiverStateRunning {
		return fmt.Errorf("receiver is already running")
	}

	// Build a fresh inner receiver (new Postgres connection) each time.
	inner, err := r.newInner(r.parentCtx, r.opts)
	if err != nil {
		return fmt.Errorf("cannot create pg receiver: %w", err)
	}

	ctx, cancel := context.WithCancel(r.parentCtx)
	done := make(chan struct{})

	r.inner = inner
	r.cancel = cancel
	r.done = done
	r.state = ReceiverStateRunning

	go func() {
		defer close(done)
		defer func() {
			r.mu.Lock()
			r.state = ReceiverStateStopped
			r.inner = nil
			r.mu.Unlock()
		}()

		if err := inner.Run(ctx); err != nil {
			r.log().Error("receiver exited with error", slog.Any("err", err))
		} else {
			r.log().Info("receiver exited cleanly")
		}
	}()

	r.log().Info("receiver started")
	return nil
}

// Stop cancels the current streaming context and blocks until the Run goroutine
// has exited. It is a no-op if the receiver is already stopped.
func (r *RestartablePgReceiver) Stop() {
	r.mu.Lock()

	if r.state == ReceiverStateStopped {
		r.mu.Unlock()
		return
	}

	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	r.log().Info("stopping receiver...")
	cancel()
	<-done
	r.log().Info("receiver stopped")
}

// State returns the current receiver state without blocking.
func (r *RestartablePgReceiver) State() ReceiverState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

// Status delegates to the inner receiver if running, otherwise returns a
// stopped-state status.
func (r *RestartablePgReceiver) Status() *StreamStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.inner == nil {
		return &StreamStatus{Running: false}
	}
	return r.inner.Status()
}

// CurrentOpenWALFileName delegates to the inner receiver if running.
// Returns "" when stopped.
func (r *RestartablePgReceiver) CurrentOpenWALFileName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.inner == nil {
		return ""
	}
	return r.inner.CurrentOpenWALFileName()
}

// Run satisfies the PgReceiveWal interface so that *RestartablePgReceiver can
// be passed to components such as ArchiveSupervisor that hold a PgReceiveWal.
// In combined mode the lifecycle is managed via Start/Stop; Run is therefore a
// no-op that blocks until ctx is cancelled.
func (r *RestartablePgReceiver) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Compile-time assertion: RestartablePgReceiver must satisfy PgReceiveWal.
var _ PgReceiveWal = (*RestartablePgReceiver)(nil)
