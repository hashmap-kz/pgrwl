package ioutils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mockCloser: logs Close and can simulate errors
type mockCloser struct {
	id   string
	log  *strings.Builder
	fail bool
}

func (m *mockCloser) Close() error {
	//nolint:staticcheck
	if m.log != nil {
		if m.fail {
			m.log.WriteString(fmt.Sprintf("[%s:ERR]", m.id))
		} else {
			m.log.WriteString(fmt.Sprintf("[%s:OK]", m.id))
		}
	}
	if m.fail {
		return fmt.Errorf("%s-close-error", m.id)
	}
	return nil
}

func TestMultiCloser_Success(t *testing.T) {
	var log strings.Builder
	data := []byte("hello world")
	reader := bytes.NewReader(data)

	closer1 := &mockCloser{id: "A", log: &log}
	closer2 := &mockCloser{id: "B", log: &log}

	mc := NewMultiCloser(reader, closer1, closer2)

	readBack, err := io.ReadAll(mc)
	require.NoError(t, err)
	assert.Equal(t, data, readBack)

	err = mc.Close()
	assert.NoError(t, err)
	assert.Equal(t, "[A:OK][B:OK]", log.String())
}

func TestMultiCloser_CloseWithErrors(t *testing.T) {
	var log strings.Builder

	reader := bytes.NewReader([]byte("fail test"))
	closer1 := &mockCloser{id: "A", log: &log, fail: true}
	closer2 := &mockCloser{id: "B", log: &log, fail: true}

	mc := NewMultiCloser(reader, closer1, closer2)

	_, err := io.Copy(io.Discard, mc)
	require.NoError(t, err)

	err = mc.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "A-close-error")
	assert.Contains(t, err.Error(), "B-close-error")
	assert.Equal(t, "[A:ERR][B:ERR]", log.String())
}

func TestMultiCloser_WithTempFile(t *testing.T) {
	f, err := os.CreateTemp("", "multi-closer-test")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	content := []byte("temporary content")
	_, err = f.Write(content)
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	mc := NewMultiCloser(f)

	readBack, err := io.ReadAll(mc)
	require.NoError(t, err)
	assert.Equal(t, content, readBack)

	err = mc.Close()
	assert.NoError(t, err)
}

func TestMultiCloser_Empty(t *testing.T) {
	reader := bytes.NewReader([]byte("data"))
	mc := NewMultiCloser(reader)

	_, err := io.ReadAll(mc)
	require.NoError(t, err)

	err = mc.Close()
	assert.NoError(t, err)
}
