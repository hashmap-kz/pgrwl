package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/hashmap-kz/pgrwl/internal/opt/shared/x/fsx"

	"github.com/hashmap-kz/storecrypt/pkg/storage"

	"github.com/hashmap-kz/pgrwl/internal/core/xlog"
)

type ServeModeService interface {
	GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error)
}

type serveModeSvc struct {
	l       *slog.Logger
	baseDir string
	storage *storage.TransformingStorage
	verbose bool
}

var _ ServeModeService = &serveModeSvc{}

type Opts struct {
	BaseDir string
	Storage *storage.TransformingStorage
	Verbose bool
}

func NewServeModeService(opts *Opts) ServeModeService {
	return &serveModeSvc{
		l:       slog.With("component", "serve-service"),
		baseDir: opts.BaseDir,
		storage: opts.Storage,
		verbose: opts.Verbose,
	}
}

func (s *serveModeSvc) log() *slog.Logger {
	if s.l != nil {
		return s.l
	}
	return slog.With("component", "serve-service")
}

func (s *serveModeSvc) GetWalFile(ctx context.Context, filename string) (io.ReadCloser, error) {
	// 1) Fast-path: check that file exists locally
	// 2) Check *.partial file locally
	// 3) Fetch from storage (if it's not nil)

	// TODO: send checksum in headers

	s.log().Debug("fetching WAL file", slog.String("filename", filename))

	// 1) trying to find local completed segment
	// 2) trying to find partial segment
	filePath := filepath.Join(s.baseDir, filename)
	partialFilePath := filePath + xlog.PartialSuffix

	s.log().Debug("wal-restore, fetching local file", slog.String("path", filePath))
	if fsx.FileExists(filePath) {
		s.log().Debug("wal-restore, found local file", slog.String("path", filePath))
		return os.Open(filePath)
	}
	if fsx.FileExists(partialFilePath) {
		s.log().Debug("wal-restore, found local partial file", slog.String("path", partialFilePath))
		return os.Open(partialFilePath)
	}

	// 3) trying remote
	if s.storage != nil {
		s.log().Debug("wal-restore, fetching remote file", slog.String("filename", filename))
		return s.storage.Get(ctx, filename)
	}

	return nil, fmt.Errorf("cannot fetch file: %s", filename)
}
