package xlog

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pglogrepl"
	"github.com/stretchr/testify/assert"
)

func TestFindStreamingStart_PartialAndComplete(t *testing.T) {
	dir := t.TempDir()
	segSize := uint64(16 * 1024 * 1024) // 16 MiB

	// Create a complete WAL file: 000000010000000000000001
	complete := "000000010000000000000001"
	assert.NoError(t, os.WriteFile(filepath.Join(dir, complete), make([]byte, segSize), 0o600))

	// Create a partial newer segment: 000000010000000000000002.partial
	partial := "000000010000000000000002.partial"
	assert.NoError(t, os.WriteFile(filepath.Join(dir, partial), make([]byte, 1*1024*1024), 0o600)) // shorter

	pgrw := &PgReceiveWal{
		BaseDir:  dir,
		WalSegSz: segSize,
	}

	lsn, tli, err := pgrw.FindStreamingStart()
	assert.NoError(t, err)

	// partial = segNo 2 -> startLSN = 2
	expectedLSN := segNoToLSN(2, segSize)
	assert.Equal(t, expectedLSN, lsn)
	assert.Equal(t, uint32(1), tli)
}

func TestFindStreamingStart_OnlyComplete(t *testing.T) {
	dir := t.TempDir()
	segSize := uint64(16 * 1024 * 1024)

	// Complete WAL segment
	name := "00000002000000000000000A"
	assert.NoError(t, os.WriteFile(filepath.Join(dir, name), make([]byte, segSize), 0o600))

	pgrw := &PgReceiveWal{
		BaseDir:  dir,
		WalSegSz: segSize,
	}

	lsn, tli, err := pgrw.FindStreamingStart()
	assert.NoError(t, err)

	// segNo = 10 -> startLSN = 11
	expectedLSN := segNoToLSN(11, segSize)
	assert.Equal(t, expectedLSN, lsn)
	assert.Equal(t, uint32(2), tli)
}

func TestFindStreamingStart_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	pgrw := &PgReceiveWal{
		BaseDir:  dir,
		WalSegSz: 16 * 1024 * 1024,
	}

	_, _, err := pgrw.FindStreamingStart()
	assert.ErrorIs(t, err, ErrNoWalEntries)
}

func TestFindStreamingStart_DifferentTimelines(t *testing.T) {
	dir := t.TempDir()
	segSize := uint64(16 * 1024 * 1024)

	// Create two WAL files with the same segment number but different timelines
	// One older (timeline 1), one newer (timeline 2)
	fileTLI1 := "00000001000000000000000A"         // TLI 1, seg 10, complete
	fileTLI2 := "00000002000000000000000A.partial" // TLI 2, seg 10, partial (preferred due to higher TLI)

	assert.NoError(t, os.WriteFile(filepath.Join(dir, fileTLI1), make([]byte, segSize), 0o600))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, fileTLI2), make([]byte, 1*1024*1024), 0o600)) // smaller partial

	pgrw := &PgReceiveWal{
		BaseDir:  dir,
		WalSegSz: segSize,
	}

	lsn, tli, err := pgrw.FindStreamingStart()
	assert.NoError(t, err)

	expectedLSN := segNoToLSN(10, segSize) // segNo = 10
	assert.Equal(t, expectedLSN, lsn)
	assert.Equal(t, uint32(2), tli) // should prefer TLI 2
}

func TestFindStreamingStart_MultipleFilesMixed(t *testing.T) {
	dir := t.TempDir()
	segSize := uint64(16 * 1024 * 1024)

	// Create a bunch of files with different segment numbers and TLIs
	files := []struct {
		tli       int
		seg       int
		partial   bool
		preferred bool // one "best" entry
	}{
		{1, 9, false, false},
		{1, 10, true, false},
		{1, 10, false, false},
		{2, 10, false, false}, // higher TLI, same seg
		{2, 11, true, false},
		{3, 11, false, true}, // best file: highest segNo, highest TLI, complete
		{3, 10, false, false},
		{3, 9, false, false},
	}

	var expectedLSN pglogrepl.LSN
	var expectedTLI uint32

	for _, f := range files {
		name := fmt.Sprintf("%08X%08X%08X", f.tli, 0, f.seg)
		if f.partial {
			name += ".partial"
		}
		path := filepath.Join(dir, name)

		contentSize := segSize
		if f.partial {
			contentSize = segSize / 4
		}

		assert.NoError(t, os.WriteFile(path, make([]byte, contentSize), 0o600))

		if f.preferred {
			//nolint:gosec
			expectedTLI = uint32(f.tli)
			if f.partial {
				expectedLSN = segNoToLSN(uint64(f.seg), segSize) //nolint:gosec
			} else {
				expectedLSN = segNoToLSN(uint64(f.seg)+1, segSize) //nolint:gosec
			}
		}
	}

	pgrw := &PgReceiveWal{
		BaseDir:  dir,
		WalSegSz: segSize,
	}

	lsn, tli, err := pgrw.FindStreamingStart()
	assert.NoError(t, err)
	assert.Equal(t, expectedTLI, tli)
	assert.Equal(t, expectedLSN, lsn)
}
