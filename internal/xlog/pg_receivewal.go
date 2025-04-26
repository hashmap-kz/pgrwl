package xlog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/jackc/pglogrepl"
)

var walFileRe = regexp.MustCompile(`^([0-9A-F]{8})([0-9A-F]{8})([0-9A-F]{8})(\.partial)?$`)

// FindStreamingStart scans baseDir for WAL files and returns (startLSN, timeline)
func FindStreamingStart(baseDir string) (pglogrepl.LSN, uint32, error) {
	type walEntry struct {
		tli       uint32
		segNo     uint64
		isPartial bool
	}

	var entries []walEntry

	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		base := filepath.Base(path)
		matches := walFileRe.FindStringSubmatch(base)
		if matches == nil {
			return nil // not a WAL file
		}

		tli, err1 := parseHex32(matches[1])
		log, err2 := parseHex32(matches[2])
		seg, err3 := parseHex32(matches[3])
		isPartial := matches[4] == ".partial"

		if err1 != nil || err2 != nil || err3 != nil {
			return nil // skip invalid names
		}

		segNo := uint64(log)*0x100000000/WalSegSz + uint64(seg)

		if !isPartial {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("could not stat file %q: %w", path, err)
			}
			if info.Size() != int64(WalSegSz) {
				fmt.Fprintf(os.Stderr, "warning: WAL segment %q has incorrect size %d, skipping\n", base, info.Size())
				return nil
			}
		}

		entries = append(entries, walEntry{
			tli:       tli,
			segNo:     segNo,
			isPartial: isPartial,
		})

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("could not read directory %q: %w", baseDir, err)
	}

	if len(entries) == 0 {
		return 0, 0, errors.New("no valid WAL segments found")
	}

	// Sort by segNo, tli, isPartial (completed > partial)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].segNo != entries[j].segNo {
			return entries[i].segNo > entries[j].segNo
		}
		if entries[i].tli != entries[j].tli {
			return entries[i].tli > entries[j].tli
		}
		return !entries[i].isPartial && entries[j].isPartial
	})

	best := entries[0]

	var startLSN pglogrepl.LSN
	if best.isPartial {
		startLSN = segNoToLSN(best.segNo)
	} else {
		startLSN = segNoToLSN(best.segNo + 1)
	}

	return startLSN, best.tli, nil
}

func parseHex32(s string) (uint32, error) {
	var v uint32
	_, err := fmt.Sscanf(s, "%08X", &v)
	return v, err
}

func segNoToLSN(segNo uint64) pglogrepl.LSN {
	return pglogrepl.LSN(segNo * WalSegSz)
}
