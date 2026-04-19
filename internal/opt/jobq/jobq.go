// Package jobq - manages a single job at a time
// 1. Start() is idempotent.
// 2. Only one worker goroutine is ever started.
// 3. Jobs are executed sequentially.
// 4. SubmitUnique(name, fn) allows only one queued-or-running job per name.
// 5. upload and retain cannot run simultaneously.
// 6. nil jobs are rejected.
// 7. job panics are recovered, logged, and do not kill the queue.
// 8. active unique jobs are released even if the job panics.
package jobq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/pgrwl/pgrwl/internal/opt/metrics/receivemetrics"
)

var (
	// ErrJobQueueFull is returned when the queue buffer is full.
	//
	// Submit and SubmitUnique are intentionally non-blocking. The queue is used
	// by periodic supervisors, where blocking the ticker loop is usually worse
	// than dropping one redundant maintenance job.
	ErrJobQueueFull = errors.New("job queue full")

	// ErrNilJob is returned when a nil job function is submitted.
	ErrNilJob = errors.New("nil job")

	// ErrJobAlreadyQueued is returned by SubmitUnique when a job with the same
	// name is already queued or currently running.
	//
	// This is the important semantic for periodic jobs such as "upload" and
	// "retain": if the previous upload has not finished yet, do not enqueue ten
	// more uploads behind it.
	ErrJobAlreadyQueued = errors.New("job already queued or running")

	// ErrNilQueue is returned when methods are called on a nil *JobQueue.
	ErrNilQueue = errors.New("nil job queue")
)

// Job is a unit of work executed by the queue.
//
// The job receives the queue context passed to Start. The queue cannot forcibly
// stop a running job. Long-running jobs must observe ctx themselves.
type Job func(ctx context.Context)

// NamedJob is the internal representation of a queued job.
type NamedJob struct {
	Name string
	Run  Job
}

// JobQueue is a small non-blocking, single-worker queue.
//
// Important properties:
//
//   - Start is idempotent.
//   - Only one worker goroutine is started.
//   - Jobs never run concurrently with each other.
//   - SubmitUnique prevents duplicate queued/running jobs by name.
//   - Submit is available for ordinary non-unique jobs.
//
// This queue is intentionally not a worker pool. For archive maintenance this is
// a feature, not a limitation: "upload" and "retain" must not mutate the same WAL
// archive state at the same time.
type JobQueue struct {
	l    *slog.Logger
	jobs chan NamedJob

	startOnce sync.Once

	mu     sync.Mutex
	active map[string]struct{}
}

// NewJobQueue creates a new queue.
//
// If bufferSize is <= 0, a buffer size of 1 is used. A tiny buffer is usually
// enough for supervisor-style periodic jobs because SubmitUnique avoids piling
// up stale copies of the same task.
func NewJobQueue(bufferSize int) *JobQueue {
	if bufferSize <= 0 {
		bufferSize = 1
	}

	return &JobQueue{
		l:      slog.With("component", "job-queue"),
		jobs:   make(chan NamedJob, bufferSize),
		active: make(map[string]struct{}),
	}
}

func (q *JobQueue) log() *slog.Logger {
	if q != nil && q.l != nil {
		return q.l
	}
	return slog.With("component", "job-queue")
}

// Start starts the single queue worker in a new goroutine.
//
// Start is safe to call multiple times. Only the first call starts a worker.
// Later calls are ignored.
//
// Prefer Run(ctx) when the queue is managed by supervisor.Supervisor, because
// Run blocks until ctx is cancelled and therefore lets the supervisor actually
// wait for the queue to stop.
func (q *JobQueue) Start(ctx context.Context) {
	if q == nil {
		return
	}

	q.startOnce.Do(func() {
		go q.Run(ctx)
	})
}

// Run processes queued jobs until ctx is cancelled.
//
// Run blocks. This makes it suitable for supervisor-managed lifecycle:
//
//	sup.Register("job-queue", func(ctx context.Context) error {
//	    q.Run(ctx)
//	    return nil
//	})
//
// Jobs are executed sequentially. This is the core guarantee of the queue:
// upload/retain jobs cannot run simultaneously.
func (q *JobQueue) Run(ctx context.Context) {
	if q == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			q.log().Info("job queue stopped", slog.Any("err", ctx.Err()))
			return

		case job := <-q.jobs:
			// If cancellation and a queued job become ready at the same time,
			// select may choose the job case. Avoid starting new work after
			// cancellation.
			if ctx.Err() != nil {
				q.log().Info(
					"job queue stopped before running job",
					slog.String("job-name", job.Name),
					slog.Any("err", ctx.Err()),
				)
				return
			}

			q.runJob(ctx, job)
		}
	}
}

func (q *JobQueue) runJob(ctx context.Context, job NamedJob) {
	if job.Run == nil {
		q.log().Error("nil job skipped", slog.String("job-name", job.Name))
		return
	}

	q.log().Info("run job", slog.String("job-name", job.Name))
	start := time.Now()

	defer func() {
		duration := time.Since(start)

		if r := recover(); r != nil {
			q.log().Error(
				"job panicked",
				slog.String("job-name", job.Name),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())),
			)
		}

		receivemetrics.M.IncJobsExecuted(job.Name)
		receivemetrics.M.ObserveJobDuration(job.Name, duration.Seconds())

		q.log().Info(
			"fin job",
			slog.String("job-name", job.Name),
			slog.Duration("duration", duration),
		)
	}()

	job.Run(ctx)
}

// Submit submits a normal job.
//
// Submit is non-blocking. If the queue buffer is full, ErrJobQueueFull is
// returned.
//
// Submit does not deduplicate jobs. For periodic archive maintenance tasks,
// prefer SubmitUnique.
func (q *JobQueue) Submit(name string, jobFunc Job) error {
	if q == nil {
		return ErrNilQueue
	}

	if jobFunc == nil {
		return fmt.Errorf("%w: %s", ErrNilJob, name)
	}

	receivemetrics.M.IncJobsSubmitted(name)

	job := NamedJob{Name: name, Run: jobFunc}

	select {
	case q.jobs <- job:
		return nil

	default:
		receivemetrics.M.IncJobsDropped(name)
		return fmt.Errorf("%w: %s", ErrJobQueueFull, name)
	}
}

// SubmitUnique submits a job only if a job with the same name is not already
// queued or running.
//
// The uniqueness window starts before the job is inserted into the queue and
// ends after the job function returns, even if the job panics.
//
// This method is intended for periodic supervisor jobs:
//
//   - upload
//   - retain
//   - scan
//   - compact
//
// Example:
//
//	err := q.SubmitUnique("upload", func(ctx context.Context) {
//	    _ = uploader.UploadPending(ctx)
//	})
//
// If "upload" is already queued or running, ErrJobAlreadyQueued is returned.
//
// SubmitUnique is also non-blocking. If the queue buffer is full, the unique
// reservation is released and ErrJobQueueFull is returned.
func (q *JobQueue) SubmitUnique(name string, jobFunc Job) error {
	if q == nil {
		return ErrNilQueue
	}

	if jobFunc == nil {
		return fmt.Errorf("%w: %s", ErrNilJob, name)
	}

	if !q.reserve(name) {
		receivemetrics.M.IncJobsDropped(name)
		return fmt.Errorf("%w: %s", ErrJobAlreadyQueued, name)
	}

	wrapped := func(ctx context.Context) {
		defer q.release(name)
		jobFunc(ctx)
	}

	receivemetrics.M.IncJobsSubmitted(name)

	job := NamedJob{Name: name, Run: wrapped}

	select {
	case q.jobs <- job:
		return nil

	default:
		q.release(name)
		receivemetrics.M.IncJobsDropped(name)
		return fmt.Errorf("%w: %s", ErrJobQueueFull, name)
	}
}

func (q *JobQueue) reserve(name string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, exists := q.active[name]; exists {
		return false
	}

	q.active[name] = struct{}{}
	return true
}

func (q *JobQueue) release(name string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.active, name)
}

// IsActive reports whether a unique job with this name is queued or running.
//
// It is mainly useful for tests, diagnostics, or debug endpoints.
func (q *JobQueue) IsActive(name string) bool {
	if q == nil {
		return false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	_, exists := q.active[name]
	return exists
}

// Len returns the number of currently queued jobs.
//
// This is a snapshot and should only be used for tests/diagnostics.
func (q *JobQueue) Len() int {
	if q == nil {
		return 0
	}

	return len(q.jobs)
}
