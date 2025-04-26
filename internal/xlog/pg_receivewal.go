package xlog

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/jackc/pglogrepl"
)

var (
	walFileRe                = regexp.MustCompile(`^([0-9A-F]{8})([0-9A-F]{8})([0-9A-F]{8})(\.partial)?$`)
	verbose                  = true
	timeToStop               = false
	endpos     pglogrepl.LSN = 0
)

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

// stop

// static bool
// stop_streaming(XLogRecPtr xlogpos, uint32 timeline, bool segment_finished)
// {
// 	static uint32 prevtimeline = 0;
// 	static XLogRecPtr prevpos = InvalidXLogRecPtr;
//
// 	/* we assume that we get called once at the end of each segment */
// 	if (verbose && segment_finished)
// 		pg_log_info("finished segment at %X/%X (timeline %u)",
// 					LSN_FORMAT_ARGS(xlogpos),
// 					timeline);
//
// 	if (!XLogRecPtrIsInvalid(endpos) && endpos < xlogpos)
// 	{
// 		if (verbose)
// 			pg_log_info("stopped log streaming at %X/%X (timeline %u)",
// 						LSN_FORMAT_ARGS(xlogpos),
// 						timeline);
// 		time_to_stop = true;
// 		return true;
// 	}
//
// 	/*
// 	 * Note that we report the previous, not current, position here. After a
// 	 * timeline switch, xlogpos points to the beginning of the segment because
// 	 * that's where we always begin streaming. Reporting the end of previous
// 	 * timeline isn't totally accurate, because the next timeline can begin
// 	 * slightly before the end of the WAL that we received on the previous
// 	 * timeline, but it's close enough for reporting purposes.
// 	 */
// 	if (verbose && prevtimeline != 0 && prevtimeline != timeline)
// 		pg_log_info("switched to timeline %u at %X/%X",
// 					timeline,
// 					LSN_FORMAT_ARGS(prevpos));
//
// 	prevtimeline = timeline;
// 	prevpos = xlogpos;
//
// 	if (time_to_stop)
// 	{
// 		if (verbose)
// 			pg_log_info("received interrupt signal, exiting");
// 		return true;
// 	}
// 	return false;
// }

func StopStreaming(xlogpos pglogrepl.LSN, timeline uint32, segmentFinished bool) bool {
	var (
		prevTimeline uint32
		prevPos      pglogrepl.LSN
	)

	if verbose && segmentFinished {
		log.Printf(
			"finished segment at %X/%X (timeline %d)",
			uint32(xlogpos>>32), uint32(xlogpos), timeline,
		)
	}

	if endpos != 0 && endpos < xlogpos {
		if verbose {
			log.Printf(
				"stopped log streaming at %X/%X (timeline %d)",
				uint32(xlogpos>>32), uint32(xlogpos), timeline,
			)
		}
		timeToStop = true
		return true
	}

	if verbose && prevTimeline != 0 && prevTimeline != timeline {
		log.Printf(
			"switched to timeline %d at %X/%X",
			timeline,
			uint32(prevPos>>32), uint32(prevPos),
		)
	}

	prevTimeline = timeline
	prevPos = xlogpos

	if timeToStop {
		if verbose {
			log.Printf("received interrupt signal, exiting")
		}
		return true
	}

	return false
}
