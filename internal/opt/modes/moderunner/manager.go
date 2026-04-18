package moderunner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/pgrwl/pgrwl/internal/opt/shared/supervisor"

	"github.com/pgrwl/pgrwl/config"
	"github.com/pgrwl/pgrwl/internal/core/xlog"
	"github.com/pgrwl/pgrwl/internal/opt/jobq"
	receiveAPI "github.com/pgrwl/pgrwl/internal/opt/modes/receivemode"
	serveAPI "github.com/pgrwl/pgrwl/internal/opt/modes/servemode"
	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
	"github.com/pgrwl/pgrwl/internal/opt/supervisors/receivesuperv"
)

// ManagerOpts holds static configuration that does not change between mode switches.
// No PGRW here — it is initialised on demand only when entering receive mode.
type ManagerOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
}

// Manager owns the mode state machine. Exactly one mode's supervisor runs at a time.
// Switch is the only entry point for transitions; it is serialised by a mutex.
type Manager struct {
	appCtx context.Context // root context; outlives individual mode supervisors
	opts   *ManagerOpts
	cfg    *config.Config
	loggr  *slog.Logger
	router *ModeRouter

	// initReceiveStorage / initServeStorage are injected so cmd layer can
	// supply the right storage-factory functions without import cycles.
	initReceiveStorage func(pgrw xlog.PgReceiveWal) *st.VariadicStorage
	initServeStorage   func() *st.VariadicStorage
	initPgrw           func() xlog.PgReceiveWal

	mu      sync.Mutex // serialises Switch calls
	sup     *supervisor.Supervisor
	current atomic.Value // string: config.ModeReceive | config.ModeServe | ""
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
	m := &Manager{
		appCtx:             appCtx,
		opts:               opts,
		cfg:                cfg,
		loggr:              slog.With("component", "mode-manager"),
		router:             router,
		initPgrw:           deps.InitPgrw,
		initReceiveStorage: deps.InitReceiveStorage,
		initServeStorage:   deps.InitServeStorage,
	}
	m.current.Store("")
	return m
}

// CurrentMode returns the name of the active mode ("receive", "serve", or "").
func (m *Manager) CurrentMode() string {
	//nolint:errcheck
	return m.current.Load().(string)
}

// Switch transitions to mode. Safe to call concurrently; calls are serialised.
func (m *Manager) Switch(mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	//nolint:errcheck
	if m.current.Load().(string) == mode {
		return fmt.Errorf("already in %s mode", mode)
	}

	m.loggr.Info("switching mode",
		//nolint:errcheck
		slog.String("from", m.current.Load().(string)),
		slog.String("to", mode),
	)

	// Stop the running supervisor before starting the next one.
	if m.sup != nil {
		m.loggr.Info("stopping current supervisor")
		m.sup.Stop()
		m.sup = nil
	}

	sup := supervisor.New(m.loggr)

	switch mode {
	case config.ModeReceive:
		pgrw := m.initPgrw()
		stor := m.initReceiveStorage(pgrw)
		jobQueue := m.registerReceiveTasks(sup, pgrw, stor)
		m.router.SwapHandler(m.buildReceiveHandler(pgrw, stor, jobQueue))

	case config.ModeServe:
		stor := m.initServeStorage()
		// Serve mode has no background tasks beyond the HTTP handler swap.
		m.router.SwapHandler(m.buildServeHandler(stor))

	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	m.sup = sup
	m.current.Store(mode)
	sup.Start(m.appCtx)

	m.loggr.Info("mode switched", slog.String("mode", mode))
	return nil
}

// Stop shuts down whichever supervisor is currently running.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sup != nil {
		m.sup.Stop()
		m.sup = nil
	}
}

// private

func (m *Manager) registerReceiveTasks(sup *supervisor.Supervisor, pgrw xlog.PgReceiveWal, stor *st.VariadicStorage) *jobq.JobQueue {
	cfg := m.cfg
	opts := m.opts

	jobQueue := jobq.NewJobQueue(5)
	jobQueue.Start(m.appCtx)

	// Critical: WAL receiver failure must stop all receive-mode tasks.
	sup.RegisterCritical("wal-receiver", func(ctx context.Context) error {
		return pgrw.Run(ctx)
	})

	if stor != nil {
		sup.Register("wal-supervisor", func(ctx context.Context) error {
			u := receivesuperv.NewArchiveSupervisor(cfg, stor, &receivesuperv.ArchiveSupervisorOpts{
				ReceiveDirectory: opts.ReceiveDirectory,
				PGRW:             pgrw,
			})
			if cfg.Receiver.Retention.Enable {
				u.RunWithRetention(ctx, jobQueue)
			} else {
				u.RunUploader(ctx, jobQueue)
			}
			return nil
		})
	}

	return jobQueue
}

func (m *Manager) buildReceiveHandler(pgrw xlog.PgReceiveWal, stor *st.VariadicStorage, jobQueue *jobq.JobQueue) http.Handler {
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
