// Package supervisor manages a group of named goroutines (tasks) with
// centralized start/stop control, panic recovery, and critical-task support.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Task is the function signature every managed goroutine must implement.
// Return a non-nil error to signal failure; return nil for a clean exit.
// The task must respect ctx.Done() and return promptly when the context
// is cancelled.
type Task func(ctx context.Context) error

// taskState tracks the lifecycle state of a single task.
type taskState int32

const (
	stateIdle    taskState = iota // registered, not yet started
	stateRunning                  // currently executing
	stateStopped                  // finished (clean exit, error, or panic)
)

// namedTask is the internal representation of a registered task.
type namedTask struct {
	name          string
	fn            Task
	cancelOnError bool // if true, any error/panic stops the whole supervisor
}

// TaskResult holds the outcome of a single task after the supervisor stops.
type TaskResult struct {
	Name     string
	Err      error // nil on clean exit
	Panicked bool  // true if the task panicked
	Panic    any   // the recovered panic value (only set when Panicked == true)
}

// Supervisor manages a group of goroutines with a unified lifecycle.
//
// Typical usage:
//
//	sup := supervisor.New(logger)
//	sup.RegisterCritical("wal-receiver", walReceiverFn)
//	sup.Register("http-server", httpServerFn)
//	sup.Start(ctx)
//	defer sup.Stop()
//
// A Supervisor must not be copied after first use.
type Supervisor struct {
	tasks []namedTask
	loggr *slog.Logger

	// lifecycle
	mu      sync.Mutex // guards cancel, results, started
	cancel  context.CancelFunc
	started bool

	// result collection
	results []TaskResult

	// wg tracks running goroutines; Wait() blocks until all are done.
	wg sync.WaitGroup

	// state per-task index (parallel slice, set atomically)
	states []atomic.Int32

	// runningCount is decremented as tasks finish; useful for health checks.
	runningCount atomic.Int32
}

// New creates a Supervisor that writes structured log output via loggr.
// If loggr is nil, a no-op logger is used.
func New(loggr *slog.Logger) *Supervisor {
	if loggr == nil {
		loggr = slog.Default()
	}
	return &Supervisor{loggr: loggr}
}

// Register adds a non-critical task. If the task returns an error or panics,
// the error is logged but the other tasks continue running.
func (s *Supervisor) Register(name string, fn Task) {
	s.addTask(name, fn, false)
}

// RegisterCritical adds a critical task. If it returns a non-nil error or
// panics, the supervisor cancels all other tasks immediately (same behaviour
// as the original cancel() call in the wal-receiver goroutine).
func (s *Supervisor) RegisterCritical(name string, fn Task) {
	s.addTask(name, fn, true)
}

func (s *Supervisor) addTask(name string, fn Task, critical bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		panic("supervisor: cannot Register after Start")
	}
	if name == "" {
		panic("supervisor: task name must not be empty")
	}
	if fn == nil {
		panic("supervisor: task function must not be nil")
	}
	for _, t := range s.tasks {
		if t.name == name {
			panic(fmt.Sprintf("supervisor: duplicate task name %q", name))
		}
	}
	s.tasks = append(s.tasks, namedTask{name: name, fn: fn, cancelOnError: critical})
}

// Start launches all registered tasks as goroutines.
// parentCtx is used as the root context; calling Stop() or a critical task
// failure will cancel a derived child context that is passed to every task.
//
// Start is idempotent after the first call — subsequent calls are no-ops.
func (s *Supervisor) Start(parentCtx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true

	ctx, cancel := context.WithCancel(parentCtx)
	s.cancel = cancel
	s.results = make([]TaskResult, len(s.tasks))
	s.states = make([]atomic.Int32, len(s.tasks))
	//nolint:gosec
	s.runningCount.Store(int32(len(s.tasks)))
	tasks := s.tasks // snapshot under lock
	s.mu.Unlock()

	for i, t := range tasks {
		i, t := i, t // capture loop vars
		s.wg.Add(1)
		go s.run(ctx, cancel, i, t)
	}
}

// run is the goroutine wrapper for a single task.
func (s *Supervisor) run(ctx context.Context, cancel context.CancelFunc, idx int, t namedTask) {
	defer s.wg.Done()
	defer s.runningCount.Add(-1)

	s.states[idx].Store(int32(stateRunning))
	s.loggr.Info("task started", slog.String("task", t.name))

	var (
		taskErr    error
		didPanic   bool
		panicValue any
	)

	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicValue = r
				taskErr = fmt.Errorf("panic: %v", r)
			}
		}()
		taskErr = t.fn(ctx)
	}()

	s.states[idx].Store(int32(stateStopped))

	// Record result (write is safe: each goroutine owns its own index).
	s.results[idx] = TaskResult{
		Name:     t.name,
		Err:      taskErr,
		Panicked: didPanic,
		Panic:    panicValue,
	}

	switch {
	case didPanic:
		s.loggr.Error("task panicked",
			slog.String("task", t.name),
			slog.Any("panic", panicValue),
		)
		if t.cancelOnError {
			s.loggr.Warn("critical task panicked — cancelling all tasks",
				slog.String("task", t.name),
			)
			cancel()
		}
	case taskErr != nil && !errors.Is(taskErr, context.Canceled) && !errors.Is(taskErr, context.DeadlineExceeded):
		s.loggr.Error("task failed",
			slog.String("task", t.name),
			slog.Any("err", taskErr),
		)
		if t.cancelOnError {
			s.loggr.Warn("critical task failed — cancelling all tasks",
				slog.String("task", t.name),
			)
			cancel()
		}
	default:
		s.loggr.Info("task stopped", slog.String("task", t.name))
	}
}

// Stop cancels all running tasks and blocks until every goroutine has returned.
// It is safe to call Stop multiple times or before Start.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// Wait blocks until all tasks have finished (equivalent to Stop without the
// cancel — useful when you want to wait for natural termination).
func (s *Supervisor) Wait() {
	s.wg.Wait()
}

// Results returns one TaskResult per registered task after all goroutines have
// finished. Call it only after Stop() or Wait() returns.
func (s *Supervisor) Results() []TaskResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TaskResult, len(s.results))
	copy(out, s.results)
	return out
}

// RunningCount returns the number of tasks still executing. Safe to call at
// any time, including while tasks are running.
func (s *Supervisor) RunningCount() int {
	return int(s.runningCount.Load())
}

// Restart stops all running tasks and starts them again under a fresh context
// derived from parentCtx.
func (s *Supervisor) Restart(parentCtx context.Context) {
	s.Stop()

	s.mu.Lock()
	s.started = false
	s.cancel = nil
	s.results = nil
	s.states = nil
	s.mu.Unlock()

	s.Start(parentCtx)
}
