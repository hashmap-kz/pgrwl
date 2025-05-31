package jobq

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var ErrJobQueueFull = errors.New("job queue full")

type Job func(ctx context.Context)

type NamedJob struct {
	Name string
	Run  func(ctx context.Context)
}

type JobQueue struct {
	l    *slog.Logger
	jobs chan NamedJob
}

func NewJobQueue(bufferSize int) *JobQueue {
	if bufferSize <= 0 {
		bufferSize = 1
	}
	return &JobQueue{
		l:    slog.With("component", "job-queue"),
		jobs: make(chan NamedJob, bufferSize),
	}
}

func (q *JobQueue) log() *slog.Logger {
	if q.l != nil {
		return q.l
	}
	return slog.With("component", "job-queue")
}

func (q *JobQueue) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-q.jobs:
				q.log().Info("run job", slog.String("job-name", job.Name))
				job.Run(ctx)
				q.log().Info("fin job", slog.String("job-name", job.Name))
			}
		}
	}()
}

func (q *JobQueue) Submit(name string, jobFunc func(ctx context.Context)) error {
	job := NamedJob{Name: name, Run: jobFunc}
	select {
	case q.jobs <- job:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrJobQueueFull, name)
	}
}
