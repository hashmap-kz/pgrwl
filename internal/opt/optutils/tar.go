package optutils

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// GetFileFromTar returns a ReadCloser for a specific file inside a tar stream.
// The caller must close the returned ReadCloser.
func GetFileFromTar(r io.Reader, target string) (io.ReadCloser, error) {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("file not found in tar")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == target {
			// Wrap tar.Reader in ReadCloser so caller can close
			pr, pw := io.Pipe()

			go func() {
				defer pw.Close()
				//nolint:gosec
				_, err := io.Copy(pw, tr)
				if err != nil {
					_ = pw.CloseWithError(err)
				}
			}()

			return pr, nil
		}
	}
}

func CreateTarReader(files []string) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer pw.Close()
		tw := tar.NewWriter(pw)
		defer tw.Close()

		for _, file := range files {
			err := func() error {
				f, err := os.Open(file)
				if err != nil {
					return err
				}
				defer f.Close()

				stat, err := f.Stat()
				if err != nil {
					return err
				}

				header, err := tar.FileInfoHeader(stat, "")
				if err != nil {
					return err
				}
				header.Name = filepath.Base(file)
				if err := tw.WriteHeader(header); err != nil {
					return err
				}
				_, err = io.Copy(tw, f)
				return err
			}()
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
	}()

	return pr
}
