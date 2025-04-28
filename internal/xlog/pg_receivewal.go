package xlog

import (
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/jackc/pglogrepl"
)

type PgReceiveWal struct {
	Verbose  bool
	BaseDir  string
	WalSegSz uint64

	timeToStop   bool
	endpos       pglogrepl.LSN
	prevTimeline uint32
	prevPos      pglogrepl.LSN
}

var _ StreamClient = &PgReceiveWal{}

var (
	walFileRe       = regexp.MustCompile(`^([0-9A-F]{8})([0-9A-F]{8})([0-9A-F]{8})(\.partial)?$`)
	ErrNoWalEntries = fmt.Errorf("no valid WAL segments found")
)

// FindStreamingStart scans baseDir for WAL files and returns (startLSN, timeline)
func (pgrw *PgReceiveWal) FindStreamingStart() (pglogrepl.LSN, uint32, error) {
	// ensure dir exists
	if err := os.MkdirAll(pgrw.BaseDir, 0o750); err != nil {
		return 0, 0, err
	}

	type walEntry struct {
		tli       uint32
		segNo     uint64
		isPartial bool
		basename  string
	}

	var entries []walEntry

	err := filepath.WalkDir(pgrw.BaseDir, func(path string, d fs.DirEntry, err error) error {
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

		segNo := uint64(log)*0x100000000/pgrw.WalSegSz + uint64(seg)

		if !isPartial {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("could not stat file %q: %w", path, err)
			}
			if uint64(info.Size()) != pgrw.WalSegSz {
				fmt.Fprintf(os.Stderr, "warning: WAL segment %q has incorrect size %d, skipping\n", base, info.Size())
				return nil
			}
		}

		entries = append(entries, walEntry{
			tli:       tli,
			segNo:     segNo,
			isPartial: isPartial,
			basename:  base,
		})

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("could not read directory %q: %w", pgrw.BaseDir, err)
	}

	if len(entries) == 0 {
		return 0, 0, ErrNoWalEntries
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
		startLSN = segNoToLSN(best.segNo, pgrw.WalSegSz)
	} else {
		startLSN = segNoToLSN(best.segNo+1, pgrw.WalSegSz)
	}

	slog.Info("found streaming start (based on WAL dir)",
		slog.String("lsn", startLSN.String()),
		slog.Uint64("tli", uint64(best.tli)),
		slog.String("wal", best.basename),
	)
	return startLSN, best.tli, nil
}

func parseHex32(s string) (uint32, error) {
	var v uint32
	_, err := fmt.Sscanf(s, "%08X", &v)
	return v, err
}

func segNoToLSN(segNo, walSegSz uint64) pglogrepl.LSN {
	return pglogrepl.LSN(segNo * walSegSz)
}

// stop_streaming
func (pgrw *PgReceiveWal) StreamStop(xlogpos pglogrepl.LSN, timeline uint32, segmentFinished bool) bool {
	if pgrw.Verbose && segmentFinished {
		log.Printf(
			"finished segment at %X/%X (timeline %d)",
			uint32(xlogpos>>32), uint32(xlogpos), timeline,
		)
	}

	if pgrw.endpos != 0 && pgrw.endpos < xlogpos {
		if pgrw.Verbose {
			log.Printf(
				"stopped log streaming at %X/%X (timeline %d)",
				uint32(xlogpos>>32), uint32(xlogpos), timeline,
			)
		}
		pgrw.timeToStop = true
		return true
	}

	if pgrw.Verbose && pgrw.prevTimeline != 0 && pgrw.prevTimeline != timeline {
		log.Printf(
			"switched to timeline %d at %X/%X",
			timeline,
			uint32(pgrw.prevPos>>32), uint32(pgrw.prevPos),
		)
	}

	pgrw.prevTimeline = timeline
	pgrw.prevPos = xlogpos

	if pgrw.timeToStop {
		if pgrw.Verbose {
			log.Printf("received interrupt signal, exiting")
		}
		return true
	}

	return false
}
