package service

import (
	"context"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/hashmap-kz/pgrwl/config"

	"github.com/hashmap-kz/pgrwl/internal/opt/modes/receivemode/model"

	"github.com/hashmap-kz/pgrwl/internal/opt/jobq"

	"github.com/hashmap-kz/pgrwl/internal/core/logger"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

type ReceiveModeService interface {
	Status() *model.PgRwlStatus
	DeleteWALsBefore(ctx context.Context, walFileName string) error
	BriefConfig(ctx context.Context) *model.BriefConfig
}

type receiveModeSvc struct {
	l           *slog.Logger
	pgrw        xlog.PgReceiveWal // direct access to running state
	baseDir     string
	runningMode string
	storage     *storage.TransformingStorage
	jobQueue    *jobq.JobQueue // optional, nil in 'serve' mode
	verbose     bool
}

var _ ReceiveModeService = &receiveModeSvc{}

type ReceiveServiceOpts struct {
	PGRW        xlog.PgReceiveWal
	BaseDir     string
	RunningMode string
	Storage     *storage.TransformingStorage
	JobQueue    *jobq.JobQueue // optional, nil in 'serve' mode
	Verbose     bool
}

func NewReceiveModeService(opts *ReceiveServiceOpts) ReceiveModeService {
	return &receiveModeSvc{
		l:           slog.With("component", "receive-service"),
		pgrw:        opts.PGRW,
		baseDir:     opts.BaseDir,
		runningMode: opts.RunningMode,
		storage:     opts.Storage,
		jobQueue:    opts.JobQueue,
		verbose:     opts.Verbose,
	}
}

func (s *receiveModeSvc) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}
	return slog.With("component", "receive-service")
}

func (s *receiveModeSvc) Status() *model.PgRwlStatus {
	s.log().Debug("querying status")

	var streamStatusResp *model.StreamStatus
	if s.pgrw != nil {
		streamStatus := s.pgrw.Status()
		streamStatusResp = &model.StreamStatus{
			Slot:         streamStatus.Slot,
			Timeline:     streamStatus.Timeline,
			LastFlushLSN: streamStatus.LastFlushLSN,
			Uptime:       streamStatus.Uptime,
			Running:      streamStatus.Running,
		}
	}
	return &model.PgRwlStatus{
		RunningMode:  s.runningMode,
		StreamStatus: streamStatusResp,
	}
}

// filterWalBefore returns a list of WAL file paths where the file name is lexically less than the cutoff WAL name.
func filterWalBefore(walFiles []string, cutoff string) []string {
	slices.Sort(walFiles)

	toDelete := []string{}
	for _, walPath := range walFiles {
		filename := filepath.Base(walPath)
		if len(filename) < xlog.XLogFileNameLen {
			continue
		}
		if !xlog.IsXLogFileName(filename[:24]) {
			continue
		}
		if filename < cutoff {
			toDelete = append(toDelete, walPath)
		}
	}
	return toDelete
}

func (s *receiveModeSvc) DeleteWALsBefore(_ context.Context, walFileName string) error {
	if s.jobQueue != nil {
		err := s.jobQueue.Submit("delete-wal-before-"+walFileName, func(_ context.Context) {
			s.log().Info("deleting WAL files")
			walFilesInStorage, err := s.storage.List(context.Background(), "")
			if err != nil {
				s.log().Error("cannot delete WAL files",
					slog.String("before", walFileName),
					slog.Any("err", err),
				)
				return
			}
			walFilesToDelete := filterWalBefore(walFilesInStorage, walFileName)
			if len(walFilesToDelete) == 0 {
				return
			}

			if s.verbose {
				s.log().LogAttrs(context.Background(), logger.LevelTrace, "begin to delete wal files")
				for _, w := range walFilesToDelete {
					s.log().LogAttrs(context.Background(), logger.LevelTrace, "wal file to delete",
						slog.String("name", w),
					)
				}
			}

			err = s.storage.DeleteAllBulk(context.Background(), walFilesToDelete)
			if err != nil {
				s.log().Error("cannot delete WAL files",
					slog.String("before", walFileName),
					slog.Any("err", err),
				)
				return
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *receiveModeSvc) BriefConfig(_ context.Context) *model.BriefConfig {
	cfg := config.Cfg()
	return &model.BriefConfig{RetentionEnable: cfg.Receiver.Retention.Enable}
}
