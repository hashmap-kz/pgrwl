package jobq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitUniqueNilQueue(t *testing.T) {
	t.Parallel()

	var q *JobQueue

	err := q.SubmitUnique("upload", func(context.Context) {})
	if !errors.Is(err, ErrNilQueue) {
		t.Fatalf("expected ErrNilQueue, got %v", err)
	}
}

func TestSubmitUniqueRejectsNilJob(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(1)

	err := q.SubmitUnique("upload", nil)
	if !errors.Is(err, ErrNilJob) {
		t.Fatalf("expected ErrNilJob, got %v", err)
	}

	if q.IsActive("upload") {
		t.Fatal("nil job must not reserve active name")
	}
}

func TestSubmitUniqueRejectsDuplicateQueuedJob(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	err := q.SubmitUnique("upload", func(context.Context) {})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	err = q.SubmitUnique("upload", func(context.Context) {})
	if !errors.Is(err, ErrJobAlreadyQueued) {
		t.Fatalf("expected ErrJobAlreadyQueued, got %v", err)
	}

	if !q.IsActive("upload") {
		t.Fatal("upload should be active while queued")
	}

	if got := q.Len(); got != 1 {
		t.Fatalf("queue len = %d, want 1", got)
	}
}

func TestSubmitUniqueReleasesReservationWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(1)

	err := q.SubmitUnique("upload", func(context.Context) {})
	if err != nil {
		t.Fatalf("submit upload: %v", err)
	}

	err = q.SubmitUnique("retain", func(context.Context) {})
	if !errors.Is(err, ErrJobQueueFull) {
		t.Fatalf("expected ErrJobQueueFull, got %v", err)
	}

	if q.IsActive("retain") {
		t.Fatal("retain reservation should be released when queue is full")
	}

	if !q.IsActive("upload") {
		t.Fatal("upload should still be active")
	}
}

func TestRunExecutesJobsSequentially(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(4)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var running atomic.Int32
	var maxRunning atomic.Int32
	var completed atomic.Int32

	for i := 0; i < 4; i++ {
		name := string(rune('a' + i))

		err := q.SubmitUnique(name, func(context.Context) {
			now := running.Add(1)
			updateMax(&maxRunning, now)

			time.Sleep(10 * time.Millisecond)

			running.Add(-1)
			completed.Add(1)
		})
		if err != nil {
			t.Fatalf("submit %s: %v", name, err)
		}
	}

	go q.Run(ctx)

	waitUntil(t, time.Second, func() bool {
		return completed.Load() == 4
	})

	cancel()

	if got := maxRunning.Load(); got != 1 {
		t.Fatalf("max concurrently running jobs = %d, want 1", got)
	}

	if got := running.Load(); got != 0 {
		t.Fatalf("running jobs = %d, want 0", got)
	}
}

func TestSubmitUniqueRejectsDuplicateWhileJobIsRunningAndAllowsAfterFinish(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobStarted := make(chan struct{})
	releaseJob := make(chan struct{})
	jobFinished := make(chan struct{})

	err := q.SubmitUnique("upload", func(context.Context) {
		close(jobStarted)
		<-releaseJob
		close(jobFinished)
	})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	go q.Run(ctx)

	select {
	case <-jobStarted:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}

	err = q.SubmitUnique("upload", func(context.Context) {})
	if !errors.Is(err, ErrJobAlreadyQueued) {
		t.Fatalf("expected ErrJobAlreadyQueued while running, got %v", err)
	}

	close(releaseJob)

	select {
	case <-jobFinished:
	case <-time.After(time.Second):
		t.Fatal("job did not finish")
	}

	waitUntil(t, time.Second, func() bool {
		return !q.IsActive("upload")
	})

	err = q.SubmitUnique("upload", func(context.Context) {})
	if err != nil {
		t.Fatalf("submit after previous job finished: %v", err)
	}
}

func TestJobPanicIsRecoveredAndReservationIsReleased(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobStarted := make(chan struct{})

	err := q.SubmitUnique("upload", func(context.Context) {
		close(jobStarted)
		panic("boom")
	})
	if err != nil {
		t.Fatalf("submit panic job: %v", err)
	}

	go q.Run(ctx)

	select {
	case <-jobStarted:
	case <-time.After(time.Second):
		t.Fatal("panic job did not start")
	}

	waitUntil(t, time.Second, func() bool {
		return !q.IsActive("upload")
	})

	err = q.SubmitUnique("upload", func(context.Context) {})
	if err != nil {
		t.Fatalf("submit after panic should succeed: %v", err)
	}
}

func TestStartIsIdempotentAndStartsOnlyOneWorker(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 16; i++ {
		q.Start(ctx)
	}

	firstStarted := make(chan struct{})
	allowFirstFinish := make(chan struct{})
	secondStarted := make(chan struct{})

	err := q.SubmitUnique("first", func(context.Context) {
		close(firstStarted)
		<-allowFirstFinish
	})
	if err != nil {
		t.Fatalf("submit first: %v", err)
	}

	err = q.SubmitUnique("second", func(context.Context) {
		close(secondStarted)
	})
	if err != nil {
		t.Fatalf("submit second: %v", err)
	}

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first job did not start")
	}

	select {
	case <-secondStarted:
		t.Fatal("second job started while first job was still running; more than one worker exists")
	case <-time.After(50 * time.Millisecond):
	}

	close(allowFirstFinish)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second job did not start after first finished")
	}
}

func TestRunCalledTwiceDoesNotStartSecondWorker(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go q.Run(ctx)
	go q.Run(ctx)

	firstStarted := make(chan struct{})
	allowFirstFinish := make(chan struct{})
	secondStarted := make(chan struct{})

	err := q.SubmitUnique("first", func(context.Context) {
		close(firstStarted)
		<-allowFirstFinish
	})
	if err != nil {
		t.Fatalf("submit first: %v", err)
	}

	err = q.SubmitUnique("second", func(context.Context) {
		close(secondStarted)
	})
	if err != nil {
		t.Fatalf("submit second: %v", err)
	}

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first job did not start")
	}

	select {
	case <-secondStarted:
		t.Fatal("second job started while first job was still running; second Run started another worker")
	case <-time.After(50 * time.Millisecond):
	}

	close(allowFirstFinish)

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second job did not start after first finished")
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		q.Run(ctx)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestQueuedUniqueJobsAreReleasedWhenQueueStopsBeforeRunningThem(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(2)

	err := q.SubmitUnique("upload", func(context.Context) {})
	if err != nil {
		t.Fatalf("submit upload: %v", err)
	}

	err = q.SubmitUnique("retain", func(context.Context) {})
	if err != nil {
		t.Fatalf("submit retain: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	q.Run(ctx)

	if q.IsActive("upload") {
		t.Fatal("upload should be released when queue stops before running queued job")
	}

	if q.IsActive("retain") {
		t.Fatal("retain should be released when queue stops before running queued job")
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("queue len = %d, want 0", got)
	}
}

func TestNilQueueHelpersAreSafe(t *testing.T) {
	t.Parallel()

	var q *JobQueue

	q.Start(context.Background())
	q.Run(context.Background())

	if q.IsActive("upload") {
		t.Fatal("nil queue should not report active job")
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("nil queue len = %d, want 0", got)
	}
}

func TestManyConcurrentSubmitUniqueCallsOnlyOneWinsPerName(t *testing.T) {
	t.Parallel()

	q := NewJobQueue(32)

	const goroutines = 32

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errs <- q.SubmitUnique("upload", func(context.Context) {})
		}()
	}

	wg.Wait()
	close(errs)

	var success int
	var duplicates int

	for err := range errs {
		switch {
		case err == nil:
			success++

		case errors.Is(err, ErrJobAlreadyQueued):
			duplicates++

		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if success != 1 {
		t.Fatalf("success count = %d, want 1", success)
	}

	if duplicates != goroutines-1 {
		t.Fatalf("duplicate count = %d, want %d", duplicates, goroutines-1)
	}
}

func updateMax(xMax *atomic.Int32, value int32) {
	for {
		old := xMax.Load()
		if value <= old {
			return
		}

		if xMax.CompareAndSwap(old, value) {
			return
		}
	}
}

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
