package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestSupervisor() *Supervisor {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.Register("", func(context.Context) error { return nil })
	if !errors.Is(err, ErrEmptyTaskName) {
		t.Fatalf("expected ErrEmptyTaskName, got %v", err)
	}

	err = sup.Register("worker", nil)
	if !errors.Is(err, ErrNilTask) {
		t.Fatalf("expected ErrNilTask, got %v", err)
	}

	err = sup.Register("worker", func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Register("worker", func(context.Context) error { return nil })
	if !errors.Is(err, ErrDuplicateTaskName) {
		t.Fatalf("expected ErrDuplicateTaskName, got %v", err)
	}
}

func TestRegisterCriticalValidation(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.RegisterCritical("", func(context.Context) error { return nil })
	if !errors.Is(err, ErrEmptyTaskName) {
		t.Fatalf("expected ErrEmptyTaskName, got %v", err)
	}

	err = sup.RegisterCritical("critical", nil)
	if !errors.Is(err, ErrNilTask) {
		t.Fatalf("expected ErrNilTask, got %v", err)
	}

	err = sup.RegisterCritical("critical", func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("register critical: %v", err)
	}

	err = sup.RegisterCritical("critical", func(context.Context) error { return nil })
	if !errors.Is(err, ErrDuplicateTaskName) {
		t.Fatalf("expected ErrDuplicateTaskName, got %v", err)
	}
}

func TestRegisterAfterStartIsRejected(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.Register("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	err = sup.Register("late-worker", func(context.Context) error { return nil })
	if !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("expected ErrRegistrationClosed, got %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestStartValidation(t *testing.T) {
	t.Parallel()

	t.Run("nil context", func(t *testing.T) {
		t.Parallel()

		sup := newTestSupervisor()

		//nolint:staticcheck
		err := sup.Start(nil)
		if !errors.Is(err, ErrNilContext) {
			t.Fatalf("expected ErrNilContext, got %v", err)
		}
	})

	t.Run("no tasks", func(t *testing.T) {
		t.Parallel()

		sup := newTestSupervisor()

		err := sup.Start(context.Background())
		if !errors.Is(err, ErrNoTasks) {
			t.Fatalf("expected ErrNoTasks, got %v", err)
		}
	})

	t.Run("already running", func(t *testing.T) {
		t.Parallel()

		sup := newTestSupervisor()

		err := sup.Register("worker", func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		if err != nil {
			t.Fatalf("register: %v", err)
		}

		err = sup.Start(context.Background())
		if err != nil {
			t.Fatalf("start: %v", err)
		}

		err = sup.Start(context.Background())
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("expected ErrAlreadyRunning, got %v", err)
		}

		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		err = sup.Stop(stopCtx)
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	})
}

func TestStopBeforeStartIsNoop(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop before start should be nil, got %v", err)
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running")
	}
}

func TestWaitBeforeStartIsNoop(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := sup.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait before start should be nil, got %v", err)
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running")
	}
}

func TestStopNilContext(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	//nolint:staticcheck
	err := sup.Stop(nil)
	if !errors.Is(err, ErrNilContext) {
		t.Fatalf("expected ErrNilContext, got %v", err)
	}
}

func TestWaitNilContext(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	//nolint:staticcheck
	err := sup.Wait(nil)
	if !errors.Is(err, ErrNilContext) {
		t.Fatalf("expected ErrNilContext, got %v", err)
	}
}

func TestRestartNilContext(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	//nolint:staticcheck
	err := sup.Restart(nil, context.Background())
	if !errors.Is(err, ErrNilContext) {
		t.Fatalf("expected ErrNilContext, got %v", err)
	}

	err = sup.Restart(context.Background(), nil)
	if !errors.Is(err, ErrNilContext) {
		t.Fatalf("expected ErrNilContext, got %v", err)
	}
}

func TestStartStopCooperativeTasks(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	const taskCount = 5

	var started atomic.Int32
	var stopped atomic.Int32

	for i := 0; i < taskCount; i++ {
		name := fmt.Sprintf("worker-%d", i)

		err := sup.Register(name, func(ctx context.Context) error {
			started.Add(1)
			<-ctx.Done()
			stopped.Add(1)
			return ctx.Err()
		})
		if err != nil {
			t.Fatalf("register: %v", err)
		}
	}

	err := sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		return started.Load() == taskCount
	})

	if !sup.Running() {
		t.Fatal("supervisor should be running")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	if started.Load() != taskCount {
		t.Fatalf("started = %d, want %d", started.Load(), taskCount)
	}

	if stopped.Load() != taskCount {
		t.Fatalf("stopped = %d, want %d", stopped.Load(), taskCount)
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running after Stop")
	}
}

func TestWaitForNaturalTaskExit(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	var ran atomic.Bool

	err := sup.Register("short-task", func(context.Context) error {
		ran.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	if !ran.Load() {
		t.Fatal("task did not run")
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running after natural exit")
	}
}

func TestNonCriticalErrorDoesNotCancelOtherTasks(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	taskErr := errors.New("temporary failure")

	failedDone := make(chan struct{})
	workerCanceled := make(chan struct{})

	err := sup.Register("non-critical-failing-task", func(context.Context) error {
		close(failedDone)
		return taskErr
	})
	if err != nil {
		t.Fatalf("register failing task: %v", err)
	}

	err = sup.Register("worker", func(ctx context.Context) error {
		<-ctx.Done()
		close(workerCanceled)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-failedDone:
	case <-time.After(time.Second):
		t.Fatal("failing task did not finish")
	}

	select {
	case <-workerCanceled:
		t.Fatal("non-critical task error canceled other worker")
	case <-time.After(50 * time.Millisecond):
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestCriticalErrorCancelsOtherTasks(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	taskErr := errors.New("critical failure")

	workerStarted := make(chan struct{})
	workerCanceled := make(chan struct{})

	err := sup.Register("worker", func(ctx context.Context) error {
		close(workerStarted)
		<-ctx.Done()
		close(workerCanceled)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	err = sup.RegisterCritical("critical", func(context.Context) error {
		<-workerStarted
		return taskErr
	})
	if err != nil {
		t.Fatalf("register critical: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	select {
	case <-workerCanceled:
	default:
		t.Fatal("worker was not canceled by critical task failure")
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running after critical failure")
	}
}

func TestCriticalContextCanceledDoesNotCauseExtraProblems(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.RegisterCritical("critical", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register critical: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running")
	}
}

func TestPanicIsRecoveredForNonCriticalTask(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	panicTaskDone := make(chan struct{})
	workerCanceled := make(chan struct{})

	err := sup.Register("panic-task", func(context.Context) error {
		defer close(panicTaskDone)
		panic("boom")
	})
	if err != nil {
		t.Fatalf("register panic task: %v", err)
	}

	err = sup.Register("worker", func(ctx context.Context) error {
		<-ctx.Done()
		close(workerCanceled)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	select {
	case <-panicTaskDone:
	case <-time.After(time.Second):
		t.Fatal("panic task did not finish")
	}

	select {
	case <-workerCanceled:
		t.Fatal("non-critical panic canceled worker")
	case <-time.After(50 * time.Millisecond):
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestPanicIsRecoveredForCriticalTaskAndCancelsOthers(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	workerStarted := make(chan struct{})
	workerCanceled := make(chan struct{})

	err := sup.Register("worker", func(ctx context.Context) error {
		close(workerStarted)
		<-ctx.Done()
		close(workerCanceled)
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register worker: %v", err)
	}

	err = sup.RegisterCritical("panic-task", func(context.Context) error {
		<-workerStarted
		panic("boom")
	})
	if err != nil {
		t.Fatalf("register panic task: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}

	select {
	case <-workerCanceled:
	default:
		t.Fatal("worker was not canceled by critical panic")
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running")
	}
}

func TestStopTimeoutWhenTaskIgnoresContext(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	release := make(chan struct{})

	err := sup.Register("bad-worker", func(context.Context) error {
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err = sup.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}

	if !sup.Running() {
		t.Fatal("supervisor should still be running after Stop timeout")
	}

	close(release)

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()

	err = sup.Stop(cleanupCtx)
	if err != nil {
		t.Fatalf("cleanup stop: %v", err)
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running after cleanup stop")
	}
}

func TestRestartStopsAndStartsAgain(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	var starts atomic.Int32

	err := sup.Register("worker", func(ctx context.Context) error {
		starts.Add(1)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		return starts.Load() == 1
	})

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Restart(stopCtx, context.Background())
	if err != nil {
		t.Fatalf("restart: %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		return starts.Load() == 2
	})

	if !sup.Running() {
		t.Fatal("supervisor should be running after Restart")
	}

	finalStopCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()

	err = sup.Stop(finalStopCtx)
	if err != nil {
		t.Fatalf("final stop: %v", err)
	}
}

func TestRestartBeforeStartStartsTasks(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	var ran atomic.Bool

	err := sup.Register("worker", func(ctx context.Context) error {
		ran.Store(true)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Restart(stopCtx, context.Background())
	if err != nil {
		t.Fatalf("restart before start: %v", err)
	}

	waitUntil(t, time.Second, ran.Load)

	if !sup.Running() {
		t.Fatal("supervisor should be running")
	}

	finalStopCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()

	err = sup.Stop(finalStopCtx)
	if err != nil {
		t.Fatalf("final stop: %v", err)
	}
}

func TestConcurrentStopCallsAreSafe(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.Register("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)

	for i := 0; i < 16; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			errs <- sup.Stop(stopCtx)
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("stop returned error: %v", err)
		}
	}

	if sup.Running() {
		t.Fatal("supervisor should not be running")
	}
}

func TestConcurrentStartCallsOnlyOneSucceeds(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	err := sup.Register("worker", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)

	for i := 0; i < 16; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			errs <- sup.Start(context.Background())
		}()
	}

	wg.Wait()
	close(errs)

	var success int
	var alreadyRunning int

	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrAlreadyRunning):
			alreadyRunning++
		default:
			t.Fatalf("unexpected start error: %v", err)
		}
	}

	if success != 1 {
		t.Fatalf("successful starts = %d, want 1", success)
	}

	if alreadyRunning != 15 {
		t.Fatalf("ErrAlreadyRunning count = %d, want 15", alreadyRunning)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Stop(stopCtx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestConcurrentRunningCallsAreSafe(t *testing.T) {
	t.Parallel()

	sup := newTestSupervisor()

	release := make(chan struct{})

	err := sup.Register("worker", func(ctx context.Context) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = sup.Start(context.Background())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	var wg sync.WaitGroup

	for i := 0; i < 16; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 1000; j++ {
				_ = sup.Running()
			}
		}()
	}

	wg.Wait()

	close(release)

	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = sup.Wait(waitCtx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
}

//nolint:unparam
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("condition was not met before timeout")
}
