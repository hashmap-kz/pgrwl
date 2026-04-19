package moderunner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	receiveAPI "github.com/pgrwl/pgrwl/internal/opt/modes/receivemode"
	serveAPI "github.com/pgrwl/pgrwl/internal/opt/modes/servemode"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/shared/supervisor"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesuperv"
)

const defaultModeStopTimeout = 30 * time.Second

// ManagerOpts holds static configuration that does not change between mode switches.
//
// No PGRW here: it is initialized on demand only when entering receive mode.
type ManagerOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
}

// Manager owns the mode state machine.
//
// Exactly one mode is active at a time.
// Receive mode has a supervisor.
// Serve mode currently has no background goroutines, so it has no supervisor.
type Manager struct {
	appCtx context.Context
	opts   *ManagerOpts
	cfg    *config.Config
	loggr  *slog.Logger
	router *ModeRouter

	// initReceiveStorage / initServeStorage are injected so the cmd layer can
	// supply storage-factory functions without import cycles.
	initReceiveStorage func(pgrw xlog.PgReceiveWal) *st.VariadicStorage
	initServeStorage   func() *st.VariadicStorage
	initPgrw           func() xlog.PgReceiveWal

	mu      sync.Mutex
	sup     *supervisor.Supervisor
	current string
}

type ManagerDeps struct {
	InitPgrw           func() xlog.PgReceiveWal
	InitReceiveStorage func(pgrw xlog.PgReceiveWal) *st.VariadicStorage
	InitServeStorage   func() *st.VariadicStorage
}

func NewManager(
	appCtx context.Context,
	opts *ManagerOpts,
	cfg *config.Config,
	router *ModeRouter,
	deps ManagerDeps,
) *Manager {
	return &Manager{
		appCtx:             appCtx,
		opts:               opts,
		cfg:                cfg,
		loggr:              slog.With("component", "mode-manager"),
		router:             router,
		initPgrw:           deps.InitPgrw,
		initReceiveStorage: deps.InitReceiveStorage,
		initServeStorage:   deps.InitServeStorage,
	}
}

// CurrentMode returns the active mode name.
//
// Possible values are:
//
//   - config.ModeReceive
//   - config.ModeServe
//   - "" when no mode is active
func (m *Manager) CurrentMode() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.current
}

// Switch transitions to mode.
//
// Switch is safe to call concurrently; calls are serialized.
//
// A default timeout is used while stopping the previous mode. This protects the
// HTTP mode-switch request from hanging forever if a task ignores ctx.Done().
func (m *Manager) Switch(mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultModeStopTimeout)
	defer cancel()

	return m.SwitchContext(ctx, mode)
}

// SwitchContext transitions to mode using ctx as the stop-wait context for the
// currently running supervisor.
func (m *Manager) SwitchContext(ctx context.Context, mode string) error {
	if ctx == nil {
		return supervisor.ErrNilContext
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if mode != config.ModeReceive && mode != config.ModeServe {
		return fmt.Errorf("unknown mode: %s", mode)
	}

	if m.current == mode {
		return fmt.Errorf("already in %s mode", mode)
	}

	m.loggr.Info("switching mode",
		slog.String("from", m.current),
		slog.String("to", mode),
	)

	if err := m.stopLocked(ctx); err != nil {
		return fmt.Errorf("stop current supervisor: %w", err)
	}

	// Be explicitly empty while the next mode is being built.
	//
	// This avoids exposing a stale receive/serve handler if constructing the
	// next mode fails after the previous mode has already been stopped.
	m.current = ""
	m.router.SwapHandler(http.NotFoundHandler())

	switch mode {
	case config.ModeReceive:
		if err := m.switchToReceiveLocked(); err != nil {
			return err
		}

	case config.ModeServe:
		m.switchToServeLocked()
	}

	m.current = mode

	m.loggr.Info("mode switched", slog.String("mode", mode))
	return nil
}

// Stop shuts down whichever supervisor is currently running.
//
// Stop also resets the router to NotFoundHandler so stopped mode handlers do not
// remain reachable.
func (m *Manager) Stop(ctx context.Context) error {
	if ctx == nil {
		return supervisor.ErrNilContext
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.stopLocked(ctx)
}

func (m *Manager) stopLocked(ctx context.Context) error {
	if m.sup != nil {
		m.loggr.Info("stopping current supervisor")

		if err := m.sup.Stop(ctx); err != nil {
			return err
		}

		m.sup = nil
	}

	m.current = ""
	m.router.SwapHandler(http.NotFoundHandler())

	return nil
}

func (m *Manager) switchToReceiveLocked() error {
	sup := supervisor.New(m.loggr)

	pgrw := m.initPgrw()
	stor := m.initReceiveStorage(pgrw)

	jobQueue, err := m.registerReceiveTasks(sup, pgrw, stor)
	if err != nil {
		return fmt.Errorf("register receive tasks: %w", err)
	}

	if err := sup.Start(m.appCtx); err != nil {
		return fmt.Errorf("start receive supervisor: %w", err)
	}

	m.sup = sup
	m.router.SwapHandler(m.buildReceiveHandler(pgrw, stor, jobQueue))

	return nil
}

func (m *Manager) switchToServeLocked() {
	stor := m.initServeStorage()

	// Serve mode has no background tasks. Do not create or start an empty
	// supervisor, because Supervisor.Start would correctly return ErrNoTasks.
	m.sup = nil
	m.router.SwapHandler(m.buildServeHandler(stor))
}

func (m *Manager) registerReceiveTasks(
	sup *supervisor.Supervisor,
	pgrw xlog.PgReceiveWal,
	stor *st.VariadicStorage,
) (*jobq.JobQueue, error) {
	cfg := m.cfg
	opts := m.opts

	jobQueue := jobq.NewJobQueue(5)

	// The job queue belongs to receive mode and must stop when receive mode
	// stops. Do not start it with m.appCtx, otherwise it can survive a
	// receive -> serve switch.
	if err := sup.Register("job-queue", func(ctx context.Context) error {
		jobQueue.Run(ctx)
		return nil
	}); err != nil {
		return nil, err
	}

	// Critical: WAL receiver failure must stop all receive-mode tasks.
	if err := sup.RegisterCritical("wal-receiver", func(ctx context.Context) error {
		return pgrw.Run(ctx)
	}); err != nil {
		return nil, err
	}

	if stor != nil {
		if err := sup.Register("wal-supervisor", func(ctx context.Context) error {
			u := receivesuperv.NewArchiveSupervisor(cfg, stor, &receivesuperv.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				PGRW:             pgrw,
			})

			u.Run(ctx, jobQueue)
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return jobQueue, nil
}

func (m *Manager) buildReceiveHandler(
	pgrw xlog.PgReceiveWal,
	stor *st.VariadicStorage,
	jobQueue *jobq.JobQueue,
) http.Handler {
	return receiveAPI.Init(&receiveAPI.ReceiveHandlerOpts{
		PGRW:     pgrw,
		BaseDir:  m.opts.ReceiveDirectory,
		Storage:  stor,
		JobQueue: jobQueue,
	})
}

func (m *Manager) buildServeHandler(stor *st.VariadicStorage) http.Handler {
	return serveAPI.Init(&serveAPI.ServeHandlerOpts{
		BaseDir: m.opts.ReceiveDirectory,
		Storage: stor,
	})
}
