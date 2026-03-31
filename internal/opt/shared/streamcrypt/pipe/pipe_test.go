package pipe

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgrwl/pgrwl/internal/opt/shared/streamcrypt/codec"
	"github.com/pgrwl/pgrwl/internal/opt/shared/streamcrypt/crypt/aesgcm"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"
)

const chunkSize = 64 * 1024

func TestChunkedGCMCrypto_SingleChunk(t *testing.T) {
	password := "strong-password"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	plain := bytes.Repeat([]byte("A"), 1024) // small payload under one chunk

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, nil, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, nil)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)

	assert.Equal(t, plain, decrypted.Bytes())
}

func TestChunkedGCMCrypto_MultiChunk(t *testing.T) {
	password := "strong-password"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	plain := bytes.Repeat([]byte("B"), chunkSize*3+1024) // multiple chunks

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, nil, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, nil)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)

	assert.Equal(t, plain, decrypted.Bytes())
}

func TestChunkedGCMCrypto_WithCompression(t *testing.T) {
	password := "hunter2"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	compressor := codec.GzipCompressor{}
	decompressor := codec.GzipDecompressor{}
	plain := bytes.Repeat([]byte("TEST-COMPRESSION-"), 1000)

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, compressor, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, decompressor)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)
	assert.Equal(t, plain, decrypted.Bytes())
}

func TestChunkedGCMCrypto_TamperDetection(t *testing.T) {
	password := "tamper-check"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	plain := bytes.Repeat([]byte("X"), 1024)

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, nil, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	data := buf.Bytes()
	data[len(data)/2] ^= 0xFF // flip a byte to simulate tampering

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(data), crypto, nil)
	assert.NoError(t, err)

	decrypted := make([]byte, 1024)
	_, err = decryptedReader.Read(decrypted)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decryption failed")
}

func TestChunkedGCMCrypto_InvalidHeader(t *testing.T) {
	invalidData := []byte("BADHEADER" + string(bytes.Repeat([]byte("A"), 100)))
	crypto := &aesgcm.ChunkedGCMCrypter{Password: "irrelevant"}

	_, err := DecryptAndDecompressOptional(bytes.NewReader(invalidData), crypto, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid file header")
}

func TestChunkedGCMCrypto_ShortData(t *testing.T) {
	crypto := &aesgcm.ChunkedGCMCrypter{Password: "p"}
	short := []byte("AEADv1short")
	_, err := DecryptAndDecompressOptional(bytes.NewReader(short), crypto, nil)
	assert.Error(t, err)
}

func TestChunkedGCMCrypto_EncryptOnlyAndDecrypt(t *testing.T) {
	password := "encrypt-only"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	plain := bytes.Repeat([]byte("C"), 4096)

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, nil, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, nil)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)
	assert.Equal(t, plain, decrypted.Bytes())
}

func TestChunkedGCMCrypto_EncryptCompressEmptyInput(t *testing.T) {
	password := "empty-input"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	compressor := codec.GzipCompressor{}
	decompressor := codec.GzipDecompressor{}

	src := bytes.NewReader(nil)
	encrypted, err := CompressAndEncryptOptional(src, compressor, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, decompressor)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(decrypted.Bytes()))
}

func TestChunkedGCMCrypto_EncryptCompressLargeInput(t *testing.T) {
	password := "large-input"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	compressor := codec.GzipCompressor{}
	decompressor := codec.GzipDecompressor{}
	plain := bytes.Repeat([]byte("Z"), chunkSize*10+777)

	src := bytes.NewReader(plain)
	encrypted, err := CompressAndEncryptOptional(src, compressor, crypto)
	assert.NoError(t, err)

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, encrypted)
	assert.NoError(t, err)

	decryptedReader, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, decompressor)
	assert.NoError(t, err)

	decrypted := new(bytes.Buffer)
	_, err = io.Copy(decrypted, decryptedReader)
	assert.NoError(t, err)
	assert.Equal(t, plain, decrypted.Bytes())
}

func TestChunkedGCMCrypto_MultipleRandomFilesWithChecksums(t *testing.T) {
	const fileCount = 20
	const fileSize = 128 * 1024 // 128 KB per file
	password := "multi-check"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	compressor := codec.GzipCompressor{}
	decompressor := codec.GzipDecompressor{}

	// Generate test data and checksums
	type testFile struct {
		data     []byte
		checksum [32]byte
	}

	files := make([]testFile, fileCount)

	for i := 0; i < fileCount; i++ {
		data := make([]byte, fileSize)
		_, err := rand.Read(data)
		assert.NoError(t, err)
		sum := sha256.Sum256(data)

		files[i] = testFile{
			data:     data,
			checksum: sum,
		}
	}

	// Encrypt + compress, then decrypt + decompress and verify checksum
	for i, f := range files {
		src := bytes.NewReader(f.data)

		encReader, err := CompressAndEncryptOptional(src, compressor, crypto)
		assert.NoError(t, err)

		var encBuf bytes.Buffer
		_, err = io.Copy(&encBuf, encReader)
		assert.NoError(t, err)

		decReader, err := DecryptAndDecompressOptional(bytes.NewReader(encBuf.Bytes()), crypto, decompressor)
		assert.NoError(t, err)

		var out bytes.Buffer
		_, err = io.Copy(&out, decReader)
		assert.NoError(t, err)

		// Compare checksums
		gotSum := sha256.Sum256(out.Bytes())
		assert.Equal(t, f.checksum, gotSum, "file #%d checksum mismatch", i)
	}
}

func TestChunkedGCMCrypto_MultipleRandomFilesWithChecksumsEncryptOnly(t *testing.T) {
	const fileCount = 20
	const fileSize = 128 * 1024 // 128 KB per file
	password := "multi-check"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}

	// Generate test data and checksums
	type testFile struct {
		data     []byte
		checksum [32]byte
	}

	files := make([]testFile, fileCount)

	for i := 0; i < fileCount; i++ {
		data := make([]byte, fileSize)
		_, err := rand.Read(data)
		assert.NoError(t, err)
		sum := sha256.Sum256(data)

		files[i] = testFile{
			data:     data,
			checksum: sum,
		}
	}

	// Encrypt + compress, then decrypt + decompress and verify checksum
	for i, f := range files {
		src := bytes.NewReader(f.data)

		encReader, err := CompressAndEncryptOptional(src, nil, crypto)
		assert.NoError(t, err)

		var encBuf bytes.Buffer
		_, err = io.Copy(&encBuf, encReader)
		assert.NoError(t, err)

		decReader, err := DecryptAndDecompressOptional(bytes.NewReader(encBuf.Bytes()), crypto, nil)
		assert.NoError(t, err)

		var out bytes.Buffer
		_, err = io.Copy(&out, decReader)
		assert.NoError(t, err)

		// Compare checksums
		gotSum := sha256.Sum256(out.Bytes())
		assert.Equal(t, f.checksum, gotSum, "file #%d checksum mismatch", i)
	}
}

func TestChunkedGCMCrypto_ChunkedReaderHandlesShortFinalChunk(t *testing.T) {
	// Full encryption/decryption but final chunk shorter than full size
	password := "short-tail"
	crypto := &aesgcm.ChunkedGCMCrypter{Password: password}
	plain := bytes.Repeat([]byte("T"), chunkSize+512)
	src := bytes.NewReader(plain)

	enc, err := CompressAndEncryptOptional(src, nil, crypto)
	assert.NoError(t, err)
	var buf bytes.Buffer
	_, err = io.Copy(&buf, enc)
	assert.NoError(t, err)

	dec, err := DecryptAndDecompressOptional(bytes.NewReader(buf.Bytes()), crypto, nil)
	assert.NoError(t, err)
	var out bytes.Buffer
	_, err = io.Copy(&out, dec)
	assert.NoError(t, err)
	assert.Equal(t, plain, out.Bytes())
}

func TestCompressEncrypt_Then_DecryptDecompress(t *testing.T) {
	tmp := t.TempDir()
	inputPath := filepath.Join(tmp, "input.txt")
	outputPath := filepath.Join(tmp, "output.txt")

	original := []byte("Hello, pgwal compression and encryption test!")

	// Write original to input file
	require.NoError(t, os.WriteFile(inputPath, original, 0o600))

	// Open source file
	srcFile, err := os.Open(inputPath)
	require.NoError(t, err)
	defer srcFile.Close()

	// Prepare compressed+encrypted stream
	compressor := &codec.GzipCompressor{}
	crypter := aesgcm.NewChunkedGCMCrypter("test-password")

	encReader, err := CompressAndEncryptOptional(srcFile, compressor, crypter)
	require.NoError(t, err)

	// Write encoded output to a file
	outFile, err := os.Create(outputPath)
	require.NoError(t, err)
	_, err = io.Copy(outFile, encReader)
	require.NoError(t, err)
	require.NoError(t, outFile.Close())

	// Read the file back and decode it
	encFile, err := os.Open(outputPath)
	require.NoError(t, err)
	defer encFile.Close()

	decReader, err := DecryptAndDecompressOptional(encFile, crypter, codec.GetDecompressor(compressor))
	require.NoError(t, err)
	defer decReader.Close()

	decoded, err := io.ReadAll(decReader)
	require.NoError(t, err)

	require.Equal(t, original, decoded)
}
