package crypt

import "io"

type Crypter interface {
	Encrypt(w io.Writer) (io.WriteCloser, error)
	Decrypt(r io.Reader) (io.Reader, error)
	FileExtension() string
	Name() string
}
