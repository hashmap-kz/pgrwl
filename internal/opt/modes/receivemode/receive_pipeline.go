package receivemode

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/grafana/dskit/services"
	st "github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"
	"github.com/hashmap-kz/pgrwl/internal/opt/supervisors/receivesuperv"
)

//nolint:revive
type ReceiveModeOpts struct {
	ReceiveDirectory string
	Slot             string
	NoLoop           bool
	ListenPort       int
	Verbose          bool
}

type pipelineCmd int

const (
	pipelineCmdStart pipelineCmd = iota + 1
	pipelineCmdStop
)

// ReceivePipelineService controls BOTH:
//   - WAL streaming loop (pgrw.Run)
//   - ArchiveSupervisor loop (RunWithRetention / RunUploader)
//
// They share a child context. When you Pause, both are canceled.
// When you Resume, both are started again.
type ReceivePipelineService struct {
	*services.BasicService
	log      *slog.Logger
	cfg      *config.Config
	pgrw     xlog.PgReceiveWal
	stor     *st.TransformingStorage
	jobQueue *jobq.JobQueue
	opts     *ReceiveModeOpts
	ctrlCh   chan pipelineCmd
	mu       sync.Mutex
	running  bool
}

func NewReceivePipelineService(
	cfg *config.Config,
	pgrw xlog.PgReceiveWal,
	stor *st.TransformingStorage,
	jobQueue *jobq.JobQueue,
	opts *ReceiveModeOpts,
	log *slog.Logger,
) *ReceivePipelineService {
	s := &ReceivePipelineService{
		log:      log.With("component", "receive-pipeline"),
		cfg:      cfg,
		pgrw:     pgrw,
		stor:     stor,
		jobQueue: jobQueue,
		opts:     opts,
		ctrlCh:   make(chan pipelineCmd),
	}

	s.BasicService = services.NewBasicService(nil, s.run, nil).
		WithName("receive-pipeline")

	return s
}

func (s *ReceivePipelineService) run(ctx context.Context) error {
	s.log.Info("receive pipeline control loop started")

	var pipeCancel context.CancelFunc

	stopPipeline := func() {
		if pipeCancel != nil {
			pipeCancel()
			pipeCancel = nil
		}
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}

	defer stopPipeline()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("receive pipeline context canceled, stopping pipeline")
			//nolint:govet
			return nil

		case cmd := <-s.ctrlCh:
			switch cmd {
			case pipelineCmdStart:
				if pipeCancel != nil {
					// Already running
					continue
				}
				s.log.Info("starting WAL + archiver pipeline")

				var pipeCtx context.Context
				//nolint:govet
				pipeCtx, pipeCancel = context.WithCancel(ctx)

				// mark as running
				s.mu.Lock()
				s.running = true
				s.mu.Unlock()

				// 1) WAL receiver goroutine
				go func() {
					if err := s.pgrw.Run(pipeCtx); err != nil && !errors.Is(err, context.Canceled) {
						s.log.Error("wal streaming failed", slog.Any("err", err))
						// You could stop pipeline or cancel root ctx here if you want fatal.
						// For now, we just log and stop pipeline on next Stop.
					} else {
						s.log.Info("wal streaming stopped")
					}
				}()

				// 2) ArchiveSupervisor goroutine (only if needed)
				if s.stor != nil {
					go func() {
						u := receivesuperv.NewArchiveSupervisor(s.cfg, s.stor, &receivesuperv.ArchiveSupervisorOpts{
							ReceiveDirectory: s.opts.ReceiveDirectory,
							PGRW:             s.pgrw,
							Verbose:          s.opts.Verbose,
						})
						if s.cfg.Receiver.Retention.Enable {
							s.log.Info("archive supervisor running with retention")
							u.RunWithRetention(pipeCtx, s.jobQueue)
						} else {
							s.log.Info("archive supervisor running (uploader only)")
							u.RunUploader(pipeCtx, s.jobQueue)
						}
						s.log.Info("archive supervisor stopped")
					}()
				}

			case pipelineCmdStop:
				s.log.Info("stopping WAL + archiver pipeline")
				stopPipeline()
			}
		}
	}
}

// Public API used by HTTP / CLI:

func (s *ReceivePipelineService) Pause() {
	select {
	case s.ctrlCh <- pipelineCmdStop:
	default:
		s.log.Warn("Pause: ctrlCh full, dropping request")
	}
}

func (s *ReceivePipelineService) Resume() {
	select {
	case s.ctrlCh <- pipelineCmdStart:
	default:
		s.log.Warn("Resume: ctrlCh full, dropping request")
	}
}

func (s *ReceivePipelineService) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}
