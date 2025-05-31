package jobq

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestJobQueue_RunSingleJob(t *testing.T) {
	queue := NewJobQueue(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ran bool
	var mu sync.Mutex

	queue.Start(ctx)

	queue.Submit("test-job", func(_ context.Context) {
		mu.Lock()
		ran = true
		mu.Unlock()
	})

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

	queue.Submit("job1", func(_ context.Context) {
		mu.Lock()
		results = append(results, "job1")
		mu.Unlock()
	})
	queue.Submit("job2", func(_ context.Context) {
		mu.Lock()
		results = append(results, "job2")
		mu.Unlock()
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, []string{"job1", "job2"}, results)
	mu.Unlock()
}

func TestJobQueue_BufferBlocksWhenFull(t *testing.T) {
	queue := NewJobQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	queue.Start(ctx)

	blocked := make(chan struct{})
	done := make(chan struct{})

	queue.Submit("job1", func(_ context.Context) {
		<-blocked // block this job so queue doesn't drain
	})

	go func() {
		queue.Submit("job2", func(_ context.Context) {
			close(done)
		})
	}()

	select {
	case <-done:
		t.Fatal("second job should be blocked until the first one finishes")
	case <-time.After(50 * time.Millisecond):
		// ok, still blocked
	}

	close(blocked) // unblock job1
	time.Sleep(100 * time.Millisecond)

	select {
	case <-done:
		assert.True(t, true)
	default:
		t.Fatal("second job should have completed after the first one unblocked")
	}
}
