package jobq

import (
	"context"
	"log/slog"
)

type Job func(ctx context.Context)

type NamedJob struct {
	Name string
	Run  func(ctx context.Context)
}
type JobQueue struct {
	jobs chan NamedJob
}

func NewJobQueue(bufferSize int) *JobQueue {
	return &JobQueue{
		jobs: make(chan NamedJob, bufferSize),
	}
}

func (q *JobQueue) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-q.jobs:
				slog.Info("running job", slog.String("name", job.Name))
				job.Run(ctx)
				slog.Info("finished job", slog.String("name", job.Name))
			}
		}
	}()
}

func (q *JobQueue) Submit(name string, jobFunc func(ctx context.Context)) {
	q.jobs <- NamedJob{
		Name: name,
		Run:  jobFunc,
	}
}
