package xlog

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePgReceiveWal is a controllable stand-in for the real pgReceiveWal.
// It records how many times Run() was called and blocks until its context
// is cancelled or stopCh is closed.
type fakePgReceiveWal struct {
	runCount atomic.Int32
	// runErr is returned by Run() if non-nil.
	runErr error
	// blockUntilCancel controls whether Run() blocks (true) or returns immediately (false).
	blockUntilCancel bool
}

func (f *fakePgReceiveWal) Run(ctx context.Context) error {
	f.runCount.Add(1)
	if f.blockUntilCancel {
		<-ctx.Done()
	}
	return f.runErr
}

func (f *fakePgReceiveWal) Status() *StreamStatus {
	return &StreamStatus{Running: true, Slot: "fake"}
}

func (f *fakePgReceiveWal) CurrentOpenWALFileName() string {
	return "fake.partial"
}

// newTestableRestartable bypasses NewPgReceiver (which needs Postgres) by
// injecting a factory that returns a fake receiver.
func newTestableRestartable(parentCtx context.Context, fake *fakePgReceiveWal) *RestartablePgReceiver {
	r := &RestartablePgReceiver{
		l:         nil, // slog.Default() used by log()
		opts:      &PgReceiveWalOpts{ReceiveDirectory: tempDir(), Slot: "test"},
		parentCtx: parentCtx,
		state:     ReceiverStateStopped,
	}
	// Patch the factory so tests don't need a real Postgres connection.
	r.newInner = func(_ context.Context, _ *PgReceiveWalOpts) (PgReceiveWal, error) {
		return fake, nil
	}
	return r
}

// tempDir returns a throwaway directory name; tests that don't touch the
// filesystem can use any non-empty string.
func tempDir() string { return "/tmp/xlog-test" }

// Tests

func TestRestartable_InitialState(t *testing.T) {
	ctx := context.Background()
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	assert.Equal(t, ReceiverStateStopped, r.State())
	assert.False(t, r.Status().Running)
	assert.Equal(t, "", r.CurrentOpenWALFileName())
}

func TestRestartable_StartAndStop(t *testing.T) {
	ctx := context.Background()
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	require.NoError(t, r.Start())
	assert.Equal(t, ReceiverStateRunning, r.State())

	// Give the goroutine a moment to enter Run().
	assert.Eventually(t, func() bool {
		return fake.runCount.Load() == 1
	}, time.Second, 10*time.Millisecond)

	// Status delegates to the fake.
	assert.True(t, r.Status().Running)
	assert.Equal(t, "fake.partial", r.CurrentOpenWALFileName())

	r.Stop()
	assert.Equal(t, ReceiverStateStopped, r.State())
	assert.False(t, r.Status().Running)
}

func TestRestartable_DoubleStartReturnsError(t *testing.T) {
	ctx := context.Background()
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	require.NoError(t, r.Start())
	defer r.Stop()

	err := r.Start()
	assert.ErrorContains(t, err, "already running")
}

func TestRestartable_StopIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	// Stop on an already-stopped receiver must not block or panic.
	done := make(chan struct{})
	go func() {
		r.Stop()
		r.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop() blocked on an already-stopped receiver")
	}
}

func TestRestartable_RestartAfterStop(t *testing.T) {
	ctx := context.Background()
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	// First run
	require.NoError(t, r.Start())
	assert.Eventually(t, func() bool { return fake.runCount.Load() == 1 }, time.Second, 10*time.Millisecond)
	r.Stop()
	assert.Equal(t, ReceiverStateStopped, r.State())

	// Second run - must succeed
	require.NoError(t, r.Start())
	assert.Eventually(t, func() bool { return fake.runCount.Load() == 2 }, time.Second, 10*time.Millisecond)
	assert.Equal(t, ReceiverStateRunning, r.State())
	r.Stop()
}

func TestRestartable_RunExitsOnInnerError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	fake := &fakePgReceiveWal{blockUntilCancel: false, runErr: boom}
	r := newTestableRestartable(ctx, fake)

	require.NoError(t, r.Start())

	// The inner Run() returns immediately with an error; the goroutine should
	// mark state as stopped.
	assert.Eventually(t, func() bool {
		return r.State() == ReceiverStateStopped
	}, time.Second, 10*time.Millisecond)
}

func TestRestartable_ParentContextCancelsRunning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakePgReceiveWal{blockUntilCancel: true}
	r := newTestableRestartable(ctx, fake)

	require.NoError(t, r.Start())
	assert.Eventually(t, func() bool { return fake.runCount.Load() == 1 }, time.Second, 10*time.Millisecond)

	// Cancelling the parent context must stop the receiver.
	cancel()

	assert.Eventually(t, func() bool {
		return r.State() == ReceiverStateStopped
	}, time.Second, 10*time.Millisecond)
}

func TestRestartable_FactoryErrorPropagates(t *testing.T) {
	ctx := context.Background()
	factoryErr := errors.New("cannot connect")

	r := &RestartablePgReceiver{
		opts:      &PgReceiveWalOpts{},
		parentCtx: ctx,
		state:     ReceiverStateStopped,
		newInner: func(_ context.Context, _ *PgReceiveWalOpts) (PgReceiveWal, error) {
			return nil, factoryErr
		},
	}

	err := r.Start()
	require.ErrorIs(t, err, factoryErr)
	assert.Equal(t, ReceiverStateStopped, r.State())
}

func TestRestartable_SatisfiesPgReceiveWalInterface(_ *testing.T) {
	// Compile-time assertion - if RestartablePgReceiver stops satisfying
	// PgReceiveWal, this line will fail to compile.
	var _ PgReceiveWal = (*RestartablePgReceiver)(nil)
}
