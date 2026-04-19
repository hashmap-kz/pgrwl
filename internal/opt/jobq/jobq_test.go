package jobq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func TestJobQueue_RunSingleJob(t *testing.T) {
	queue := NewJobQueue(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran bool
	var mu sync.Mutex

	queue.Start(ctx)

	err := queue.Submit("test-job", func(_ context.Context) {
		mu.Lock()
		ran = true
		mu.Unlock()
	})
	assert.NoError(t, err)

	time.Sleep(100 * time.Millisecond) // allow job to run

	mu.Lock()
	assert.True(t, ran, "job should have been executed")
	mu.Unlock()
}

func TestJobQueue_JobOrder(t *testing.T) {
	queue := NewJobQueue(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var results []string
	var mu sync.Mutex

	queue.Start(ctx)

	assert.NoError(t, queue.Submit("job1", func(_ context.Context) {
		mu.Lock()
		results = append(results, "job1")
		mu.Unlock()
	}))
	assert.NoError(t, queue.Submit("job2", func(_ context.Context) {
		mu.Lock()
		results = append(results, "job2")
		mu.Unlock()
	}))
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, []string{"job1", "job2"}, results)
	mu.Unlock()
}

func TestSubmit_ExecutesJob(t *testing.T) {
	queue := NewJobQueue(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	var ran bool

	queue.Start(ctx)

	wg.Add(1)
	err := queue.Submit("test-job", func(_ context.Context) {
		defer wg.Done()
		ran = true
	})
	assert.NoError(t, err)

	wg.Wait()
	assert.True(t, ran, "job should have run")
}

func TestSubmit_ReturnsErrWhenQueueIsFull(t *testing.T) {
	queue := NewJobQueue(1)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	blocked := make(chan struct{})

	// Fill the queue with a blocking job
	err := queue.Submit("job1", func(_ context.Context) {
		<-blocked // block forever
	})
	assert.NoError(t, err)

	// Try to submit another job — should fail
	err = queue.Submit("job2", func(_ context.Context) {})
	assert.ErrorIs(t, err, ErrJobQueueFull)
	assert.Contains(t, err.Error(), "job2")
	close(blocked) // cleanup
}

// v2

func TestNewJobQueueUsesDefaultBufferWhenInvalid(t *testing.T) {
	q := NewJobQueue(0)

	require.NotNil(t, q)
	assert.Equal(t, 0, q.Len())

	err := q.Submit("one", func(_ context.Context) {})
	require.NoError(t, err)

	err = q.Submit("two", func(_ context.Context) {})
	require.ErrorIs(t, err, ErrJobQueueFull)
}

func TestSubmitRejectsNilQueue(t *testing.T) {
	var q *JobQueue

	err := q.Submit("upload", func(_ context.Context) {})

	require.ErrorIs(t, err, ErrNilQueue)
}

func TestSubmitUniqueRejectsNilQueue(t *testing.T) {
	var q *JobQueue

	err := q.SubmitUnique("upload", func(_ context.Context) {})

	require.ErrorIs(t, err, ErrNilQueue)
}

func TestSubmitRejectsNilJob(t *testing.T) {
	q := NewJobQueue(1)

	err := q.Submit("upload", nil)

	require.ErrorIs(t, err, ErrNilJob)
	assert.Equal(t, 0, q.Len())
}

func TestSubmitUniqueRejectsNilJob(t *testing.T) {
	q := NewJobQueue(1)

	err := q.SubmitUnique("upload", nil)

	require.ErrorIs(t, err, ErrNilJob)
	assert.False(t, q.IsActive("upload"))
	assert.Equal(t, 0, q.Len())
}

func TestSubmitReturnsQueueFull(t *testing.T) {
	q := NewJobQueue(1)

	err := q.Submit("first", func(_ context.Context) {})
	require.NoError(t, err)

	err = q.Submit("second", func(_ context.Context) {})

	require.ErrorIs(t, err, ErrJobQueueFull)
	assert.Equal(t, 1, q.Len())
}

func TestSubmitUniqueReturnsQueueFullAndReleasesReservation(t *testing.T) {
	q := NewJobQueue(1)

	err := q.Submit("ordinary", func(_ context.Context) {})
	require.NoError(t, err)

	err = q.SubmitUnique("upload", func(_ context.Context) {})
	require.ErrorIs(t, err, ErrJobQueueFull)

	assert.False(t, q.IsActive("upload"))

	// If reservation was not released, this would return ErrJobAlreadyQueued
	// instead of ErrJobQueueFull.
	err = q.SubmitUnique("upload", func(_ context.Context) {})
	require.ErrorIs(t, err, ErrJobQueueFull)
}

func TestSubmitUniqueRejectsDuplicateQueuedJob(t *testing.T) {
	q := NewJobQueue(10)

	err := q.SubmitUnique("upload", func(_ context.Context) {})
	require.NoError(t, err)

	err = q.SubmitUnique("upload", func(_ context.Context) {})

	require.ErrorIs(t, err, ErrJobAlreadyQueued)
	assert.True(t, q.IsActive("upload"))
	assert.Equal(t, 1, q.Len())
}

func TestSubmitUniqueAllowsDifferentJobNames(t *testing.T) {
	q := NewJobQueue(10)

	err := q.SubmitUnique("upload", func(_ context.Context) {})
	require.NoError(t, err)

	err = q.SubmitUnique("retain", func(_ context.Context) {})
	require.NoError(t, err)

	assert.True(t, q.IsActive("upload"))
	assert.True(t, q.IsActive("retain"))
	assert.Equal(t, 2, q.Len())
}

func TestSubmitUniqueRejectsDuplicateRunningJob(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	release := make(chan struct{})

	q.Start(ctx)

	err := q.SubmitUnique("upload", func(_ context.Context) {
		close(started)
		<-release
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	err = q.SubmitUnique("upload", func(_ context.Context) {})

	require.ErrorIs(t, err, ErrJobAlreadyQueued)
	assert.True(t, q.IsActive("upload"))

	close(release)

	require.Eventually(t, func() bool {
		return !q.IsActive("upload")
	}, time.Second, time.Millisecond)
}

func TestSubmitUniqueReleasesAfterSuccessfulRun(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int64
	done := make(chan struct{})

	q.Start(ctx)

	err := q.SubmitUnique("upload", func(_ context.Context) {
		runs.Add(1)
		close(done)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	require.Eventually(t, func() bool {
		return !q.IsActive("upload")
	}, time.Second, time.Millisecond)

	err = q.SubmitUnique("upload", func(_ context.Context) {
		runs.Add(1)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return runs.Load() == 2
	}, time.Second, time.Millisecond)
}

func TestSubmitUniqueReleasesAfterPanic(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstStarted := make(chan struct{})
	secondDone := make(chan struct{})

	q.Start(ctx)

	err := q.SubmitUnique("upload", func(_ context.Context) {
		close(firstStarted)
		panic("boom")
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-firstStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	require.Eventually(t, func() bool {
		return !q.IsActive("upload")
	}, time.Second, time.Millisecond)

	err = q.SubmitUnique("upload", func(_ context.Context) {
		close(secondDone)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-secondDone:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestPanicDoesNotKillWorker(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	afterPanicRan := make(chan struct{})

	q.Start(ctx)

	err := q.Submit("panic", func(_ context.Context) {
		panic("bad job")
	})
	require.NoError(t, err)

	err = q.Submit("after-panic", func(_ context.Context) {
		close(afterPanicRan)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-afterPanicRan:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestStartIsIdempotentAndJobsNeverRunConcurrently(t *testing.T) {
	q := NewJobQueue(100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// This is the chaos-prevention test.
	//
	// Even if someone accidentally calls Start many times, only one worker must
	// run. Therefore maxRunning must stay exactly 1.
	for i := 0; i < 32; i++ {
		q.Start(ctx)
	}

	const totalJobs = 50

	var wg sync.WaitGroup
	wg.Add(totalJobs)

	var running atomic.Int64
	var maxRunning atomic.Int64

	for i := 0; i < totalJobs; i++ {
		err := q.Submit(fmt.Sprintf("job-%d", i), func(_ context.Context) {
			now := running.Add(1)
			updateMax(&maxRunning, now)

			time.Sleep(2 * time.Millisecond)

			running.Add(-1)
			wg.Done()
		})
		require.NoError(t, err)
	}

	waitDone(t, &wg, time.Second)

	assert.Equal(t, int64(1), maxRunning.Load())
	assert.Equal(t, int64(0), running.Load())
}

func TestUploadAndRetainNeverRunSimultaneously(t *testing.T) {
	q := NewJobQueue(100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx)

	const totalPairs = 30

	var wg sync.WaitGroup
	wg.Add(totalPairs * 2)

	var runningArchiveMutators atomic.Int64
	var maxRunningArchiveMutators atomic.Int64

	archiveJob := func(_ context.Context) {
		now := runningArchiveMutators.Add(1)
		updateMax(&maxRunningArchiveMutators, now)

		time.Sleep(2 * time.Millisecond)

		runningArchiveMutators.Add(-1)
		wg.Done()
	}

	for i := 0; i < totalPairs; i++ {
		require.NoError(t, q.Submit("upload", archiveJob))
		require.NoError(t, q.Submit("retain", archiveJob))
	}

	waitDone(t, &wg, time.Second)

	assert.Equal(t, int64(1), maxRunningArchiveMutators.Load())
	assert.Equal(t, int64(0), runningArchiveMutators.Load())
}

func TestSubmitUniquePreventsTickerSpamForUpload(t *testing.T) {
	q := NewJobQueue(100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx)

	started := make(chan struct{})
	release := make(chan struct{})

	var runs atomic.Int64
	var alreadyQueued int64

	err := q.SubmitUnique("upload", func(_ context.Context) {
		runs.Add(1)
		close(started)
		<-release
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	for i := 0; i < 100; i++ {
		err := q.SubmitUnique("upload", func(_ context.Context) {
			runs.Add(1)
		})
		if errors.Is(err, ErrJobAlreadyQueued) {
			alreadyQueued++
			continue
		}
		require.NoError(t, err)
	}

	assert.Equal(t, int64(100), alreadyQueued)
	assert.Equal(t, int64(1), runs.Load())

	close(release)

	require.Eventually(t, func() bool {
		return !q.IsActive("upload")
	}, time.Second, time.Millisecond)
}

func TestSubmitUniqueAllowsUploadAndRetainToQueueButTheyStillRunSequentially(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q.Start(ctx)

	var wg sync.WaitGroup
	wg.Add(2)

	var running atomic.Int64
	var maxRunning atomic.Int64

	job := func(_ context.Context) {
		now := running.Add(1)
		updateMax(&maxRunning, now)

		time.Sleep(10 * time.Millisecond)

		running.Add(-1)
		wg.Done()
	}

	require.NoError(t, q.SubmitUnique("upload", job))
	require.NoError(t, q.SubmitUnique("retain", job))

	waitDone(t, &wg, time.Second)

	assert.Equal(t, int64(1), maxRunning.Load())
	assert.False(t, q.IsActive("upload"))
	assert.False(t, q.IsActive("retain"))
}

func TestContextCancellationStopsWorker(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())

	firstRan := make(chan struct{})
	secondRan := make(chan struct{})

	q.Start(ctx)

	err := q.Submit("first", func(_ context.Context) {
		close(firstRan)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-firstRan:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	cancel()

	require.Eventually(t, func() bool {
		return ctx.Err() != nil
	}, time.Second, time.Millisecond)

	// Submitting after cancellation can still enqueue because Submit is only a
	// queue operation. But the worker should not execute it after it has stopped.
	err = q.Submit("second", func(_ context.Context) {
		close(secondRan)
	})
	require.NoError(t, err)

	select {
	case <-secondRan:
		t.Fatal("job ran after queue context was canceled")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestRunningJobReceivesCanceledContext(t *testing.T) {
	q := NewJobQueue(10)

	ctx, cancel := context.WithCancel(context.Background())

	jobStarted := make(chan struct{})
	jobObservedCancel := make(chan struct{})

	q.Start(ctx)

	err := q.Submit("long-running", func(_ context.Context) {
		close(jobStarted)
		<-ctx.Done()
		close(jobObservedCancel)
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		select {
		case <-jobStarted:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)

	cancel()

	require.Eventually(t, func() bool {
		select {
		case <-jobObservedCancel:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestIsActiveAndLenAreNilSafe(t *testing.T) {
	var q *JobQueue

	assert.False(t, q.IsActive("upload"))
	assert.Equal(t, 0, q.Len())
}

func updateMax(xMax *atomic.Int64, value int64) {
	for {
		current := xMax.Load()
		if value <= current {
			return
		}

		if xMax.CompareAndSwap(current, value) {
			return
		}
	}
}

func waitDone(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timeout waiting for jobs to finish")
	}
}
