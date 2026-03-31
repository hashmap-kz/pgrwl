package tarx

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type untarFunc func(r io.Reader, dest string) error

func TestUntar(t *testing.T) {
	t.Run("Untar", func(t *testing.T) {
		runUntarCommonTests(t, Untar)
	})
}

func runUntarCommonTests(t *testing.T, fn untarFunc) {
	t.Run("extracts directories and files correctly", func(t *testing.T) {
		dest := t.TempDir()

		archive := buildTar(t, []tarEntry{
			dirEntry("a"),
			dirEntry("a/b"),
			fileEntry("a/hello.txt", "hello"),
			fileEntry("a/b/world.txt", "world"),
			fileEntry("root.txt", "root-file"),
		})

		err := fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertDirExists(t, filepath.Join(dest, "a"))
		assertDirExists(t, filepath.Join(dest, "a", "b"))

		assertFileContent(t, filepath.Join(dest, "a", "hello.txt"), "hello")
		assertFileContent(t, filepath.Join(dest, "a", "b", "world.txt"), "world")
		assertFileContent(t, filepath.Join(dest, "root.txt"), "root-file")
	})

	t.Run("creates parent directories for regular files even if dir headers are absent", func(t *testing.T) {
		dest := t.TempDir()

		archive := buildTar(t, []tarEntry{
			fileEntry("x/y/z/file.txt", "nested-data"),
		})

		err := fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertDirExists(t, filepath.Join(dest, "x"))
		assertDirExists(t, filepath.Join(dest, "x", "y"))
		assertDirExists(t, filepath.Join(dest, "x", "y", "z"))
		assertFileContent(t, filepath.Join(dest, "x", "y", "z", "file.txt"), "nested-data")
	})

	t.Run("overwrites existing file contents", func(t *testing.T) {
		dest := t.TempDir()
		target := filepath.Join(dest, "same.txt")

		err := os.WriteFile(target, []byte("old-content"), 0o600)
		assert.NoError(t, err)

		archive := buildTar(t, []tarEntry{
			fileEntry("same.txt", "new-content"),
		})

		err = fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertFileContent(t, target, "new-content")
	})

	t.Run("skips unsupported types like symlink", func(t *testing.T) {
		dest := t.TempDir()

		archive := buildTar(t, []tarEntry{
			fileEntry("normal.txt", "ok"),
			symlinkEntry("link.txt", "normal.txt"),
		})

		err := fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertFileContent(t, filepath.Join(dest, "normal.txt"), "ok")

		_, statErr := os.Lstat(filepath.Join(dest, "link.txt"))
		assert.Error(t, statErr)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("returns error on invalid tar stream", func(t *testing.T) {
		dest := t.TempDir()

		err := fn(strings.NewReader("this-is-not-a-tar"), dest)
		assert.Error(t, err)
	})

	t.Run("extracts empty file correctly", func(t *testing.T) {
		dest := t.TempDir()

		archive := buildTar(t, []tarEntry{
			fileEntry("empty.txt", ""),
		})

		err := fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertFileContent(t, filepath.Join(dest, "empty.txt"), "")
	})

	t.Run("handles repeated dir entries", func(t *testing.T) {
		dest := t.TempDir()

		archive := buildTar(t, []tarEntry{
			dirEntry("dup"),
			dirEntry("dup"),
			fileEntry("dup/file.txt", "data"),
		})

		err := fn(bytes.NewReader(archive), dest)
		assert.NoError(t, err)

		assertDirExists(t, filepath.Join(dest, "dup"))
		assertFileContent(t, filepath.Join(dest, "dup", "file.txt"), "data")
	})
}

// go test ./... -bench=Untar -benchmem

func BenchmarkUntar(b *testing.B) {
	archive := buildBenchmarkTar(b, 2000, 4096)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dest := b.TempDir()
		if err := Untar(bytes.NewReader(archive), dest); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUntarDeepTree(b *testing.B) {
	archive := buildDeepTreeBenchmarkTar(b, 5000, 1024)

	b.Run("Untar", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dest := b.TempDir()
			if err := Untar(bytes.NewReader(archive), dest); err != nil {
				b.Fatal(err)
			}
		}
	})
}

type tarEntry struct {
	header *tar.Header
	body   []byte
}

func dirEntry(name string) tarEntry {
	name = strings.TrimSuffix(name, "/") + "/"
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeDir,
			Mode:     0o700,
		},
	}
}

func fileEntry(name, content string) tarEntry {
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o600,
			Size:     int64(len(content)),
		},
		body: []byte(content),
	}
}

func symlinkEntry(name, target string) tarEntry {
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeSymlink,
			Linkname: target,
			Mode:     0o777,
		},
	}
}

func buildTar(tb testing.TB, entries []tarEntry) []byte {
	tb.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, e := range entries {
		err := tw.WriteHeader(e.header)
		assert.NoError(tb, err)

		if len(e.body) > 0 {
			_, err = tw.Write(e.body)
			assert.NoError(tb, err)
		}
	}

	err := tw.Close()
	assert.NoError(tb, err)

	return buf.Bytes()
}

func buildBenchmarkTar(tb testing.TB, files, fileSize int) []byte {
	tb.Helper()

	//nolint:gosec
	rng := rand.New(rand.NewSource(42))

	entries := make([]tarEntry, 0, files+32)

	for i := 0; i < 20; i++ {
		entries = append(entries, dirEntry(fmt.Sprintf("dir-%02d", i)))
	}

	for i := 0; i < files; i++ {
		dir := fmt.Sprintf("dir-%02d/subdir-%02d", i%20, i%50)
		name := fmt.Sprintf("%s/file-%06d.txt", dir, i)
		entries = append(entries, fileEntryBytes(name, randomBytes(rng, fileSize)))
	}

	return buildTar(tb, entries)
}

func buildDeepTreeBenchmarkTar(tb testing.TB, files, fileSize int) []byte {
	tb.Helper()

	//nolint:gosec
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	entries := make([]tarEntry, 0, files)

	for i := 0; i < files; i++ {
		name := fmt.Sprintf("a/b/c/d/e/f/g/h/i/j/file-%06d.txt", i)
		entries = append(entries, fileEntryBytes(name, randomBytes(rng, fileSize)))
	}

	return buildTar(tb, entries)
}

func fileEntryBytes(name string, content []byte) tarEntry {
	return tarEntry{
		header: &tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0o600,
			Size:     int64(len(content)),
		},
		body: content,
	}
}

func randomBytes(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	_, _ = rng.Read(b)
	return b
}

func assertFileContent(t *testing.T, path, expected string) {
	t.Helper()

	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	assert.NoError(t, err)
	assert.True(t, info.IsDir(), "expected %q to be a directory", path)
}
