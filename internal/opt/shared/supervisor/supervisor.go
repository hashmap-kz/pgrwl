// Package supervisor manages a fixed group of named goroutines with
// centralized start, stop, restart, panic recovery, and critical-task support.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"sync"
)

var (
	ErrNilContext         = errors.New("supervisor: context must not be nil")
	ErrNoTasks            = errors.New("supervisor: no tasks registered")
	ErrEmptyTaskName      = errors.New("supervisor: task name must not be empty")
	ErrNilTask            = errors.New("supervisor: task function must not be nil")
	ErrDuplicateTaskName  = errors.New("supervisor: duplicate task name")
	ErrRegistrationClosed = errors.New("supervisor: task registration is closed")
	ErrAlreadyRunning     = errors.New("supervisor: already running")
)

// Task is a supervised goroutine body.
//
// A task must respect ctx.Done() and return promptly when the context is
// cancelled. The supervisor cannot forcibly kill goroutines.
type Task func(ctx context.Context) error

type task struct {
	name     string
	fn       Task
	critical bool
}

// Supervisor manages a fixed group of goroutines.
//
// Register all tasks first, then call Start. After the first successful Start,
// the task set is sealed and cannot be changed.
//
// A Supervisor is safe for concurrent Start, Stop, Restart, and Running calls.
// Wait intentionally does not hold the lifecycle mutex while blocking, so a
// concurrent Stop can still interrupt a long wait.
type Supervisor struct {
	loggr *slog.Logger

	// lifeMu serializes mutating lifecycle operations.
	lifeMu sync.Mutex

	// mu guards fields below.
	mu sync.Mutex

	tasks  []task
	sealed bool

	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// New creates a Supervisor.
//
// If loggr is nil, log output is discarded.
func New(loggr *slog.Logger) *Supervisor {
	if loggr == nil {
		loggr = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &Supervisor{
		loggr: loggr,
	}
}

// Register adds a non-critical task.
//
// If a non-critical task returns an error or panics, the supervisor logs it,
// but other tasks continue running.
func (s *Supervisor) Register(name string, fn Task) error {
	return s.register(name, fn, false)
}

// RegisterCritical adds a critical task.
//
// If a critical task returns a real error or panics, the supervisor cancels all
// other tasks.
//
// context.Canceled and context.DeadlineExceeded are treated as normal shutdown.
func (s *Supervisor) RegisterCritical(name string, fn Task) error {
	return s.register(name, fn, true)
}

func (s *Supervisor) register(name string, fn Task, critical bool) error {
	if name == "" {
		return ErrEmptyTaskName
	}
	if fn == nil {
		return ErrNilTask
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sealed {
		return ErrRegistrationClosed
	}

	for _, t := range s.tasks {
		if t.name == name {
			return fmt.Errorf("%w: %q", ErrDuplicateTaskName, name)
		}
	}

	s.tasks = append(s.tasks, task{
		name:     name,
		fn:       fn,
		critical: critical,
	})

	return nil
}

// Start starts all registered tasks.
func (s *Supervisor) Start(parent context.Context) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	return s.start(parent)
}

// Stop cancels all running tasks and waits until they return.
//
// If ctx expires before all tasks stop, Stop returns ctx.Err(). The supervisor
// keeps running internally, and Stop can be called again later.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	return s.stop(ctx)
}

// Wait waits until all tasks exit naturally.
//
// Wait does not cancel the supervisor context.
//
// Wait does not hold lifeMu while blocking. This is intentional: another
// goroutine must still be able to call Stop while somebody is waiting.
func (s *Supervisor) Wait(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	done := s.done
	s.mu.Unlock()

	return wait(ctx, done)
}

// Restart stops the current run and starts the same task set again.
func (s *Supervisor) Restart(stopCtx, startCtx context.Context) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	if err := s.stop(stopCtx); err != nil {
		return err
	}

	return s.start(startCtx)
}

// Running reports whether the supervisor currently has running tasks.
func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.running
}

func (s *Supervisor) start(parent context.Context) error {
	if parent == nil {
		return ErrNilContext
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return ErrAlreadyRunning
	}

	if len(s.tasks) == 0 {
		return ErrNoTasks
	}

	ctx, cancel := context.WithCancel(parent)

	done := make(chan struct{})
	tasks := append([]task(nil), s.tasks...)

	s.sealed = true
	s.running = true
	s.cancel = cancel
	s.done = done

	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for _, t := range tasks {
		t := t

		go func() {
			defer wg.Done()
			s.run(ctx, cancel, t)
		}()
	}

	go func() {
		wg.Wait()

		// Clear the state before closing done.
		//
		// This ordering keeps Start simple after a natural exit: once all tasks
		// are finished, Running can become false immediately. The goroutine does
		// not touch supervisor state after close(done), so a later Start cannot
		// be accidentally cleared by this old run.
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.done = nil
		s.mu.Unlock()

		close(done)
	}()

	return nil
}

func (s *Supervisor) stop(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}

	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return nil
	}

	cancel := s.cancel
	done := s.done

	s.mu.Unlock()

	cancel()

	return wait(ctx, done)
}

func (s *Supervisor) run(ctx context.Context, cancel context.CancelFunc, t task) {
	s.loggr.Info("task started",
		slog.String("task", t.name),
		slog.Bool("critical", t.critical),
	)

	err := runSafely(ctx, t.fn)

	switch {
	case err == nil:
		s.loggr.Info("task stopped",
			slog.String("task", t.name),
			slog.Bool("critical", t.critical),
		)

	case isShutdownError(err):
		s.loggr.Info("task stopped",
			slog.String("task", t.name),
			slog.Bool("critical", t.critical),
			slog.Any("err", err),
		)

	default:
		s.loggr.Error("task failed",
			slog.String("task", t.name),
			slog.Bool("critical", t.critical),
			slog.Any("err", err),
		)

		if t.critical {
			s.loggr.Warn("critical task failed; cancelling supervisor",
				slog.String("task", t.name),
			)
			cancel()
		}
	}
}

func runSafely(ctx context.Context, fn Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()

	return fn(ctx)
}

func wait(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isShutdownError(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
