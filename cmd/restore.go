package cmd

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/hashmap-kz/pgrwl/config"
	"github.com/hashmap-kz/pgrwl/internal/opt/supervisor"
)

func RestoreBaseBackup(ctx context.Context, cfg *config.Config, id, dest string) error {
	loggr := slog.With(slog.String("component", "restore"), slog.String("id", id))

	// TODO: refuse if dest dir exists and not empty

	loggr.Info("destination", slog.String("dest", filepath.ToSlash(dest)))
	if err := os.MkdirAll(filepath.ToSlash(dest), 0o750); err != nil {
		return err
	}

	// setup storage
	stor, err := supervisor.SetupStorage(&supervisor.SetupStorageOpts{
		BaseDir: filepath.ToSlash(cfg.Main.Directory),
		SubPath: filepath.ToSlash(filepath.Join(config.BaseBackupSubpath, id)),
	})
	if err != nil {
		return err
	}

	// cat backup path -> fixed directory + timestamp (id)
	list, err := stor.List(ctx, "")
	if err != nil {
		return err
	}

	// TODO: tablespaces
	// untar archives
	for _, f := range list {
		slog.Debug("restoring file", slog.String("path", filepath.ToSlash(f)))

		rc, err := stor.Get(ctx, f)
		if err != nil {
			return fmt.Errorf("get %s: %w", id, err)
		}
		// TODO: log() func
		if err := untar(rc, dest, loggr); err != nil {
			return fmt.Errorf("untar %s: %w", id, err)
		}
		if err := rc.Close(); err != nil {
			return err
		}
	}
	return nil
}

func untar(r io.Reader, dest string, loggr *slog.Logger) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		//nolint:gosec
		target := filepath.ToSlash(filepath.Join(dest, hdr.Name))
		loggr.Debug("tar target", slog.String("path", target))

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			//nolint:gosec
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			// skip other types (e.g., symlinks in tar)
		}
	}
	return nil
}
