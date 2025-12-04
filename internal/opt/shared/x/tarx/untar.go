package tarx

import (
	"archive/tar"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

func Untar(r io.Reader, dest string) error {
	loggr := slog.With(slog.String("component", "restore-untar"), slog.String("dest", dest))

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
