package aesgcm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"github.com/pgrwl/pgrwl/internal/opt/shared/streamcrypt/crypt"

	"golang.org/x/crypto/argon2"
)

// Constants

const (
	chunkSize    = 64 * 1024
	nonceSize    = 12 // AES-GCM requires a 12-byte (96-bit) nonce for optimal performance.
	saltSize     = 16 // A 128-bit salt is standard in key derivation (like Argon2, PBKDF2, scrypt).
	keySize      = 32 // AES-256 requires a 256-bit key = 32 bytes.
	headerPrefix = "AEADv1"
)

// Key Derivation

func GeneratePBEKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, keySize)
}

func GenerateRandomNBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// Chunked GCM Crypter

type ChunkedGCMCrypter struct {
	Password string
}

var _ crypt.Crypter = &ChunkedGCMCrypter{}

func NewChunkedGCMCrypter(password string) crypt.Crypter {
	return &ChunkedGCMCrypter{
		Password: password,
	}
}

func (c *ChunkedGCMCrypter) FileExtension() string {
	return ".aes"
}

func (c *ChunkedGCMCrypter) Name() string {
	return "aes-256-gcm"
}

func (c *ChunkedGCMCrypter) Encrypt(w io.Writer) (io.WriteCloser, error) {
	salt, err := GenerateRandomNBytes(saltSize)
	if err != nil {
		return nil, err
	}
	key := GeneratePBEKey(c.Password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if _, err := w.Write([]byte(headerPrefix)); err != nil {
		return nil, err
	}
	if _, err := w.Write(salt); err != nil {
		return nil, err
	}

	return &gcmChunkedWriter{
		aead:     aead,
		w:        w,
		buf:      make([]byte, 0, chunkSize),
		chunkNum: 0,
	}, nil
}

type gcmChunkedWriter struct {
	aead     cipher.AEAD
	w        io.Writer
	buf      []byte
	chunkNum uint64
}

func (g *gcmChunkedWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		space := chunkSize - len(g.buf)
		if space > len(p) {
			space = len(p)
		}
		g.buf = append(g.buf, p[:space]...)
		p = p[space:]
		total += space

		if len(g.buf) == chunkSize {
			if err := g.flush(); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func (g *gcmChunkedWriter) Close() error {
	if len(g.buf) > 0 {
		if err := g.flush(); err != nil {
			return err
		}
	}
	return nil
}

func (g *gcmChunkedWriter) flush() error {
	nonce := make([]byte, nonceSize)
	binary.BigEndian.PutUint64(nonce[4:], g.chunkNum)
	ciphertext := g.aead.Seal(nil, nonce, g.buf, nil)

	if _, err := g.w.Write(nonce); err != nil {
		return err
	}
	if _, err := g.w.Write(ciphertext); err != nil {
		return err
	}

	g.chunkNum++
	g.buf = g.buf[:0]
	return nil
}

func (c *ChunkedGCMCrypter) Decrypt(r io.Reader) (io.Reader, error) {
	header := make([]byte, len(headerPrefix)+saltSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if string(header[:len(headerPrefix)]) != headerPrefix {
		return nil, errors.New("invalid file header")
	}
	salt := header[len(headerPrefix):]
	key := GeneratePBEKey(c.Password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &gcmChunkedReader{
		aead:     aead,
		r:        r,
		chunkNum: 0,
		buf:      nil,
	}, nil
}

type gcmChunkedReader struct {
	aead     cipher.AEAD
	r        io.Reader
	chunkNum uint64
	buf      []byte
}

func (g *gcmChunkedReader) Read(p []byte) (int, error) {
	if len(g.buf) == 0 {
		nonce := make([]byte, nonceSize)
		if _, err := io.ReadFull(g.r, nonce); err != nil {
			return 0, err
		}

		ciphertext := make([]byte, chunkSize+g.aead.Overhead())
		n, err := io.ReadFull(g.r, ciphertext)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return 0, err
		}
		ciphertext = ciphertext[:n]

		plaintext, err := g.aead.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return 0, errors.New("decryption failed: tampering or corruption detected")
		}
		g.buf = plaintext
		g.chunkNum++
	}

	n := copy(p, g.buf)
	g.buf = g.buf[n:]
	return n, nil
}
