package aesgcm

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

func TestChunkedGCMCrypto_SaltReadFailure(t *testing.T) {
	password := "."
	crypter := &ChunkedGCMCrypter{Password: password}
	r := bytes.NewReader([]byte{}) // too short to contain salt
	decReader, err := crypter.Decrypt(r)

	assert.Error(t, err)
	assert.Nil(t, decReader)
}

func TestChunkedGCMCrypto_EncryptFunction_HeaderAndSalt(t *testing.T) {
	password := "header-test"
	crypto := &ChunkedGCMCrypter{Password: password}
	var out bytes.Buffer

	writer, err := crypto.Encrypt(&out)
	assert.NoError(t, err)
	assert.NotNil(t, writer)

	// Write and close with dummy data
	_, err = writer.Write([]byte("abc"))
	assert.NoError(t, err)
	assert.NoError(t, writer.Close())

	written := out.Bytes()
	assert.True(t, bytes.HasPrefix(written, []byte("AEADv1")), "header prefix missing")
	assert.True(t, len(written) > len("AEADv1")+saltSize, "not enough data written")
}

func TestChunkedGCMCrypto_EncryptWriteFlushCloseBehavior(t *testing.T) {
	password := "flush-check"
	crypto := &ChunkedGCMCrypter{Password: password}
	var out bytes.Buffer

	writer, err := crypto.Encrypt(&out)
	assert.NoError(t, err)
	assert.NotNil(t, writer)

	// Write data less than chunk size
	sample := bytes.Repeat([]byte("X"), 100)
	_, err = writer.Write(sample)
	assert.NoError(t, err)

	// Flush remaining data
	err = writer.Close()
	assert.NoError(t, err)
	assert.Greater(t, len(out.Bytes()), 0)
}

func TestChunkedGCMCrypto_DecryptFunction_InvalidHeader(t *testing.T) {
	password := "bad-header"
	crypto := &ChunkedGCMCrypter{Password: password}
	data := append([]byte("BADHDR"), make([]byte, 100)...) // malformed header
	reader, err := crypto.Decrypt(bytes.NewReader(data))
	assert.Nil(t, reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid file header")
}

func TestChunkedGCMCrypto_ChunkedWriterFlushChunkBoundary(t *testing.T) {
	password := "boundary-check"
	crypto := &ChunkedGCMCrypter{Password: password}
	var out bytes.Buffer

	writer, err := crypto.Encrypt(&out)
	assert.NoError(t, err)

	data := bytes.Repeat([]byte("A"), chunkSize)
	n, err := writer.Write(data)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.NoError(t, writer.Close())
}

func TestChunkedGCMCrypto_ChunkedWriterMultipleFlushes(t *testing.T) {
	password := "multi-flush"
	crypto := &ChunkedGCMCrypter{Password: password}
	var out bytes.Buffer

	writer, err := crypto.Encrypt(&out)
	assert.NoError(t, err)

	chunk := bytes.Repeat([]byte("Z"), chunkSize)
	for i := 0; i < 3; i++ {
		_, err := writer.Write(chunk)
		assert.NoError(t, err)
	}
	assert.NoError(t, writer.Close())
	assert.Greater(t, len(out.Bytes()), 3*chunkSize) // includes nonce + tag overheads
}

func TestChunkedGCMCrypto_EncryptDecryptMultipleRandomFiles(t *testing.T) {
	tmpDir := t.TempDir()
	numFiles := 50
	maxSize := 512 * 1024 // 512 KB max

	password := "test-password"
	crypter := &ChunkedGCMCrypter{Password: password}

	for i := 0; i < numFiles; i++ {
		// Random file size (including zero)
		size := randomInt(0, maxSize)
		original := make([]byte, size)
		_, err := rand.Read(original)
		assert.NoError(t, err)

		// Compute original hash
		originalHash := sha256.Sum256(original)

		// Encrypt to buffer
		var encrypted bytes.Buffer
		encWriter, err := crypter.Encrypt(&encrypted)
		assert.NoError(t, err)
		_, err = encWriter.Write(original)
		assert.NoError(t, err)
		assert.NoError(t, encWriter.Close())

		// Decrypt from buffer
		decReader, err := crypter.Decrypt(bytes.NewReader(encrypted.Bytes()))
		assert.NoError(t, err)
		decrypted, err := io.ReadAll(decReader)
		assert.NoError(t, err)

		// Compute hash of decrypted
		decryptedHash := sha256.Sum256(decrypted)

		// Assert hash matches
		assert.Equal(t, originalHash, decryptedHash, "file %d: hash mismatch", i)

		// Optionally write a debug copy if the test fails
		if !assert.Equal(t, original, decrypted) {
			//nolint:errcheck
			_ = os.WriteFile(filepath.Join(tmpDir, "fail-original.bin"), original, 0o600)
			//nolint:errcheck
			_ = os.WriteFile(filepath.Join(tmpDir, "fail-decrypted.bin"), decrypted, 0o600)
			t.Fatalf("mismatch in file %d", i)
		}
	}
}

func randomInt(xmin, xmax int) int {
	if xmax <= xmin {
		return xmin
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return xmin + int(b[0])%(xmax-xmin)
}

// benchmark
// go test -bench=. -benchmem -gcflags=-m github.com/hashmap-kz/streaming-compress-encrypt/pkg/crypt/aesgcm

func BenchmarkChunkedGCM_Encrypt(b *testing.B) {
	b.ReportAllocs()

	data := bytes.Repeat([]byte("A"), 16*1024*1024) // 16 MiB input
	password := "benchmark-secret"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		crypter := NewChunkedGCMCrypter(password)

		w, err := crypter.Encrypt(&out)
		if err != nil {
			b.Fatalf("Encrypt setup failed: %v", err)
		}

		if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
			b.Fatalf("Encrypt failed: %v", err)
		}
		if err := w.Close(); err != nil {
			b.Fatalf("Encrypt close failed: %v", err)
		}
	}
}

func BenchmarkChunkedGCM_Decrypt(b *testing.B) {
	b.ReportAllocs()

	data := bytes.Repeat([]byte("A"), 16*1024*1024) // 16 MiB input
	password := "benchmark-secret"

	// Encrypt once before benchmark to reuse ciphertext
	var buf bytes.Buffer
	crypter := NewChunkedGCMCrypter(password)
	w, err := crypter.Encrypt(&buf)
	require.NoError(b, err)
	_, err = io.Copy(w, bytes.NewReader(data))
	require.NoError(b, err)
	_ = w.Close()
	encryptedData := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(encryptedData)
		crypter := NewChunkedGCMCrypter(password)
		decReader, err := crypter.Decrypt(r)
		if err != nil {
			b.Fatalf("Decrypt setup failed: %v", err)
		}
		if _, err := io.Copy(io.Discard, decReader); err != nil {
			b.Fatalf("Decrypt failed: %v", err)
		}
	}
}

// v2

func TestChunkedGCMCrypto_EncryptDecryptRoundtrip(t *testing.T) {
	data := bytes.Repeat([]byte("hello world "), 10000) // ~120 KB
	var buf bytes.Buffer

	crypter := NewChunkedGCMCrypter("s3cr3t")
	w, err := crypter.Encrypt(&buf)
	require.NoError(t, err)

	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := crypter.Decrypt(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	result, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, data, result)
}

func TestChunkedGCMCrypto_DecryptCorrupted(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 10000)
	var buf bytes.Buffer

	crypter := NewChunkedGCMCrypter("pw")
	w, err := crypter.Encrypt(&buf)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	if err != nil {
		return
	}
	w.Close()

	cipher := buf.Bytes()
	cipher[len(cipher)-10] ^= 0xFF // flip a byte near the end

	r, err := crypter.Decrypt(bytes.NewReader(cipher))
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	require.ErrorContains(t, err, "decryption failed")
}

func TestChunkedGCMCrypto_EmptyInput(t *testing.T) {
	var buf bytes.Buffer
	crypter := NewChunkedGCMCrypter("pw")

	w, err := crypter.Encrypt(&buf)
	require.NoError(t, err)
	w.Close()

	r, err := crypter.Decrypt(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, 0, len(out))
}

func TestChunkedGCMCrypto_PartialChunkDecryption(t *testing.T) {
	var buf bytes.Buffer
	crypter := NewChunkedGCMCrypter("pw")
	w, err := crypter.Encrypt(&buf)
	require.NoError(t, err)
	_, err = w.Write([]byte("short"))
	require.NoError(t, err)
	w.Close()

	encrypted := buf.Bytes()
	cut := encrypted[:len(encrypted)-5] // truncate end
	r, err := crypter.Decrypt(bytes.NewReader(cut))
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	require.Error(t, err) // should fail
}

func TestChunkedGCMCrypto_EncryptDecryptRoundtripRangeLoop(t *testing.T) {
	const maxSize = 100
	crypter := NewChunkedGCMCrypter("s3cr3t")

	pattern := func() []byte {
		buf := make([]byte, 256)
		for i := range buf {
			buf[i] = byte(i)
		}
		return buf
	}()

	for size := 0; size <= maxSize; size++ {
		// Create deterministic test data: 0x00, 0x01, ..., 0xFF, repeated
		data := make([]byte, size)
		for i := 0; i < size; {
			n := copy(data[i:], pattern)
			i += n
		}

		var buf bytes.Buffer

		// Encrypt
		w, err := crypter.Encrypt(&buf)
		require.NoError(t, err, "encrypt init failed for size=%d", size)

		_, err = w.Write(data)
		require.NoError(t, err, "write failed for size=%d", size)
		require.NoError(t, w.Close(), "close failed for size=%d", size)

		// Decrypt
		r, err := crypter.Decrypt(bytes.NewReader(buf.Bytes()))
		require.NoError(t, err, "decrypt init failed for size=%d", size)

		result, err := io.ReadAll(r)
		require.NoError(t, err, "read failed for size=%d", size)
		require.Equal(t, data, result, "decrypted output mismatch at size=%d", size)
	}
}
