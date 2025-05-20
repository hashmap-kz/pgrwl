package optutils

import (
	"archive/tar"
	"errors"
	"io"
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
				_, err := io.Copy(pw, tr)
				if err != nil {
					_ = pw.CloseWithError(err)
				}
			}()

			return pr, nil
		}
	}
}
