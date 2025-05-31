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
