package backupsv

import (
	"context"
	"fmt"
	"log/slog"

	st "github.com/pgrwl/pgrwl/internal/opt/shared/storecrypt"
)

type WALCleaner interface {
	DeleteBefore(ctx context.Context, keepFromWAL string) error
}

type walCleaner struct {
	l       *slog.Logger
	opts    *Opts
	walStor *st.VariadicStorage
}

var _ WALCleaner = &walCleaner{}

func NewWALCleaner(opts *Opts, l *slog.Logger, walStore *st.VariadicStorage) WALCleaner {
	if l == nil {
		l = slog.With(slog.String("component", "wal-cleaner"))
	}

	return &walCleaner{
		l:       l,
		opts:    opts,
		walStor: walStore,
	}
}

func (c *walCleaner) DeleteBefore(ctx context.Context, keepFromWAL string) error {
	if keepFromWAL == "" {
		return fmt.Errorf("keepFromWAL is empty")
	}

	wals, err := c.walStor.ListInfoRaw(ctx, "")
	if err != nil {
		return fmt.Errorf("list WAL archive: %w", err)
	}

	deleted := 0
	kept := 0

	for _, wal := range wals {
		name, history, ok := normalizeWALFilename(wal.Path)
		if !ok {
			kept++
			continue
		}

		// Timeline history files are tiny and important for timeline switching.
		// Keep them for now.
		if history {
			kept++
			continue
		}

		if !walBefore(name, keepFromWAL) {
			kept++
			continue
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		c.l.Info("deleting old WAL",
			slog.String("wal", name),
			slog.String("path", wal.Path),
			slog.String("keep_from", keepFromWAL),
		)

		if err := c.walStor.Delete(ctx, wal.Path); err != nil {
			return fmt.Errorf("delete WAL %s: %w", wal.Path, err)
		}

		deleted++
	}

	c.l.Info("WAL retention completed",
		slog.String("keep_from", keepFromWAL),
		slog.Int("deleted_wals", deleted),
		slog.Int("kept_wals", kept),
	)

	return nil
}
