package supervisor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helpers

var noopLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

// blockUntilCancelled is a task that blocks until its context is cancelled.
func blockUntilCancelled(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// taskThatErrors returns an error immediately.
func taskThatErrors(err error) Task {
	return func(_ context.Context) error { return err }
}

// taskThatPanics panics immediately.
func taskThatPanics(msg string) Task {
	return func(_ context.Context) error { panic(msg) }
}

// taskThatSignals closes done when started, then blocks until ctx is done.
func taskThatSignals(done chan<- struct{}) Task {
	return func(ctx context.Context) error {
		close(done)
		<-ctx.Done()
		return nil
	}
}

// shortTimeout returns a context that times out after 2 s (safety net for tests).
func shortTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// basic lifecycle

func TestStart_RunsAllTasks(t *testing.T) {
	var count atomic.Int32
	sup := New(noopLogger)

	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		sup.Register(name, func(ctx context.Context) error {
			count.Add(1)
			<-ctx.Done()
			return nil
		})
	}

	ctx := shortTimeout(t)
	sup.Start(ctx)

	// Give goroutines a moment to start.
	time.Sleep(20 * time.Millisecond)
	if got := count.Load(); got != 3 {
		t.Fatalf("expected 3 tasks running, got %d", got)
	}

	sup.Stop()
}

func TestStop_WaitsForAllTasks(t *testing.T) {
	done := make(chan struct{}, 3)
	sup := New(noopLogger)

	for _, name := range []string{"a", "b", "c"} {
		name := name
		sup.Register(name, func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(10 * time.Millisecond) // simulate cleanup
			done <- struct{}{}
			return nil
		})
	}

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Stop()

	if len(done) != 3 {
		t.Fatalf("Stop returned before all tasks finished; got %d/3 done", len(done))
	}
}

func TestStop_BeforeStart_IsNoop(_ *testing.T) {
	sup := New(noopLogger)
	// Should not panic or block.
	sup.Stop()
}

func TestStart_IsIdempotent(t *testing.T) {
	var count atomic.Int32
	sup := New(noopLogger)
	sup.Register("a", func(ctx context.Context) error {
		count.Add(1)
		<-ctx.Done()
		return nil
	})

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Start(ctx) // second call must be a no-op
	sup.Start(ctx) // third call must be a no-op

	time.Sleep(20 * time.Millisecond)
	if got := count.Load(); got != 1 {
		t.Fatalf("expected 1 task started, got %d (Start is not idempotent)", got)
	}
	sup.Stop()
}

// critical tasks

func TestCriticalTask_ErrorCancelsOthers(t *testing.T) {
	sup := New(noopLogger)

	criticalErr := errors.New("critical failure")
	criticalStarted := make(chan struct{})

	// critical task: signals it started, then returns an error
	sup.RegisterCritical("critical", func(_ context.Context) error {
		close(criticalStarted)
		return criticalErr
	})

	// companion task: blocks until its context is cancelled
	companionDone := make(chan struct{})
	sup.Register("companion", func(ctx context.Context) error {
		<-ctx.Done()
		close(companionDone)
		return nil
	})

	ctx := shortTimeout(t)
	sup.Start(ctx)

	select {
	case <-companionDone:
		// good — companion was cancelled because critical task failed
	case <-ctx.Done():
		t.Fatal("timed out: companion task was not cancelled after critical task error")
	}

	sup.Stop()

	results := sup.Results()
	for _, r := range results {
		if r.Name == "critical" && !errors.Is(r.Err, criticalErr) {
			t.Errorf("expected critical task err=%v, got %v", criticalErr, r.Err)
		}
	}
}

func TestCriticalTask_PanicCancelsOthers(t *testing.T) {
	sup := New(noopLogger)

	sup.RegisterCritical("critical", taskThatPanics("boom"))

	companionDone := make(chan struct{})
	sup.Register("companion", func(ctx context.Context) error {
		<-ctx.Done()
		close(companionDone)
		return nil
	})

	ctx := shortTimeout(t)
	sup.Start(ctx)

	select {
	case <-companionDone:
		// good
	case <-ctx.Done():
		t.Fatal("timed out: companion was not cancelled after critical panic")
	}

	sup.Stop()

	results := sup.Results()
	for _, r := range results {
		if r.Name == "critical" {
			if !r.Panicked {
				t.Error("expected Panicked=true for critical task")
			}
			if r.Panic != "boom" {
				t.Errorf("expected panic value 'boom', got %v", r.Panic)
			}
		}
	}
}

func TestNonCriticalTask_ErrorDoesNotCancelOthers(t *testing.T) {
	sup := New(noopLogger)

	sup.Register("failing", taskThatErrors(errors.New("non-critical error")))

	companionRunning := make(chan struct{})
	sup.Register("companion", taskThatSignals(companionRunning))

	ctx := shortTimeout(t)
	sup.Start(ctx)

	// companion must still be running after the failing task exits
	select {
	case <-companionRunning:
		// good — companion started and is still blocked
	case <-ctx.Done():
		t.Fatal("timed out waiting for companion to start")
	}

	// Give the failing task time to exit and confirm companion is still alive.
	time.Sleep(30 * time.Millisecond)
	if sup.RunningCount() < 1 {
		t.Error("companion task should still be running")
	}

	sup.Stop()
}

func TestNonCriticalTask_PanicDoesNotCancelOthers(t *testing.T) {
	sup := New(noopLogger)

	sup.Register("panicking", taskThatPanics("non-critical panic"))

	companionRunning := make(chan struct{})
	sup.Register("companion", taskThatSignals(companionRunning))

	ctx := shortTimeout(t)
	sup.Start(ctx)

	select {
	case <-companionRunning:
	case <-ctx.Done():
		t.Fatal("timed out waiting for companion to start")
	}

	time.Sleep(30 * time.Millisecond)
	if sup.RunningCount() < 1 {
		t.Error("companion task should still be running after non-critical panic")
	}

	sup.Stop()
}

// context cancellation

func TestParentContextCancellation_StopsAll(t *testing.T) {
	sup := New(noopLogger)

	done := make(chan struct{}, 2)
	for _, name := range []string{"a", "b"} {
		name := name
		sup.Register(name, func(ctx context.Context) error {
			<-ctx.Done()
			done <- struct{}{}
			return nil
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	time.Sleep(20 * time.Millisecond)
	cancel() // cancel the parent
	sup.Wait()

	if len(done) != 2 {
		t.Fatalf("expected 2 tasks to stop, got %d", len(done))
	}
}

// results

func TestResults_CleanExit(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("quick", func(_ context.Context) error { return nil })

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Wait()

	results := sup.Results()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Name != "quick" {
		t.Errorf("expected name 'quick', got %q", r.Name)
	}
	if r.Err != nil {
		t.Errorf("expected nil error, got %v", r.Err)
	}
	if r.Panicked {
		t.Error("expected Panicked=false")
	}
}

func TestResults_ErrorRecorded(t *testing.T) {
	sup := New(noopLogger)
	sentinel := errors.New("sentinel")
	sup.Register("err-task", taskThatErrors(sentinel))

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Wait()

	results := sup.Results()
	if !errors.Is(results[0].Err, sentinel) {
		t.Errorf("expected sentinel error, got %v", results[0].Err)
	}
}

func TestResults_PanicRecorded(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("panic-task", taskThatPanics("oops"))

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Wait()

	results := sup.Results()
	r := results[0]
	if !r.Panicked {
		t.Error("expected Panicked=true")
	}
	if r.Panic != "oops" {
		t.Errorf("expected panic value 'oops', got %v", r.Panic)
	}
	if r.Err == nil {
		t.Error("expected non-nil Err wrapping the panic")
	}
}

func TestResults_ContextCancelledIsNotAnError(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("ctx-task", blockUntilCancelled)

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Stop()

	results := sup.Results()
	// task returns nil after ctx cancel, so Err must be nil
	if results[0].Err != nil {
		t.Errorf("expected nil Err for clean ctx-cancelled task, got %v", results[0].Err)
	}
}

// RunningCount

func TestRunningCount(t *testing.T) {
	sup := New(noopLogger)

	ready := make(chan struct{})
	sup.Register("long", func(ctx context.Context) error {
		close(ready)
		<-ctx.Done()
		return nil
	})
	sup.Register("quick", func(_ context.Context) error { return nil })

	ctx := shortTimeout(t)
	sup.Start(ctx)

	<-ready // long task has started

	time.Sleep(20 * time.Millisecond) // quick task has likely finished

	if c := sup.RunningCount(); c > 2 {
		t.Errorf("RunningCount %d > 2 (impossible)", c)
	}

	sup.Stop()

	if c := sup.RunningCount(); c != 0 {
		t.Errorf("expected RunningCount=0 after Stop, got %d", c)
	}
}

// Restart

func TestRestart_TasksRunTwice(t *testing.T) {
	var count atomic.Int32
	sup := New(noopLogger)
	sup.Register("counter", func(ctx context.Context) error {
		count.Add(1)
		<-ctx.Done()
		return nil
	})

	ctx := shortTimeout(t)
	sup.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	sup.Restart(ctx)
	time.Sleep(20 * time.Millisecond)

	if got := count.Load(); got != 2 {
		t.Fatalf("expected task to run 2 times after Restart, got %d", got)
	}
	sup.Stop()
}

// registration guards

func TestRegister_AfterStart_Panics(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("a", blockUntilCancelled)

	ctx := shortTimeout(t)
	sup.Start(ctx)
	defer sup.Stop()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when registering after Start")
		}
	}()
	sup.Register("b", blockUntilCancelled)
}

func TestRegister_EmptyName_Panics(t *testing.T) {
	sup := New(noopLogger)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for empty task name")
		}
	}()
	sup.Register("", blockUntilCancelled)
}

func TestRegister_NilFn_Panics(t *testing.T) {
	sup := New(noopLogger)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil task function")
		}
	}()
	sup.Register("a", nil)
}

func TestRegister_DuplicateName_Panics(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("a", blockUntilCancelled)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate task name")
		}
	}()
	sup.Register("a", blockUntilCancelled)
}

// nil logger

func TestNew_NilLogger_UsesDefault(t *testing.T) {
	sup := New(nil) // must not panic
	sup.Register("a", func(_ context.Context) error { return nil })

	ctx := shortTimeout(t)
	sup.Start(ctx)
	sup.Wait()
}

// concurrent Stop calls

func TestStop_Concurrent_IsRaceFree(t *testing.T) {
	sup := New(noopLogger)
	sup.Register("a", blockUntilCancelled)

	ctx := shortTimeout(t)
	sup.Start(ctx)

	// Fire multiple Stop() calls concurrently; should not race or panic.
	var wg2 sync.WaitGroup // local wg, not the supervisor's
	for i := 0; i < 10; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			sup.Stop()
		}()
	}
	wg2.Wait()
}
