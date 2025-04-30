package xlog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"

	"pgreceivewal5/internal/fsync"

	"github.com/jackc/pgx/v5/pgconn"

	"pgreceivewal5/internal/conv"

	"github.com/jackc/pglogrepl"
)

type PgReceiveWal struct {
	BaseDir     string
	WalSegSz    uint64
	Conn        *pgconn.PgConn
	ConnStrRepl string
	SlotName    string

	StopCh       chan struct{} // <- signal channel
	endpos       pglogrepl.LSN
	prevTimeline uint32
	prevPos      pglogrepl.LSN
}

var _ StreamClient = &PgReceiveWal{}

var (
	walFileRe       = regexp.MustCompile(`^([0-9A-F]{8})([0-9A-F]{8})([0-9A-F]{8})(\.partial)?$`)
	ErrNoWalEntries = fmt.Errorf("no valid WAL segments found")
)

func (pgrw *PgReceiveWal) SetupSignalHandler() {
	pgrw.StopCh = make(chan struct{})

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		s := <-sig
		slog.Info("received signal", slog.String("signal", s.String()))
		close(pgrw.StopCh)
	}()
}

// StreamLog the main loop of WAL receiving, any error FATAL
func (pgrw *PgReceiveWal) StreamLog(ctx context.Context) error {
	var err error

	// 1
	if pgrw.Conn == nil {
		pgrw.Conn, err = pgconn.Connect(context.Background(), pgrw.ConnStrRepl)
		if err != nil {
			slog.Error("cannot establish connection", slog.Any("err", err))
			// not a fatal error, a reconnect loop will handle it
			return nil
		}
	}

	walSegSz := pgrw.WalSegSz

	// 3
	var slotRestartInfo *ReadReplicationSlotResultResult
	_, err = GetSlotInformation(pgrw.Conn, pgrw.SlotName)
	if err != nil {
		if errors.Is(err, ErrSlotDoesNotExist) {
			slog.Info("creating replication slot", slog.String("name", pgrw.SlotName))
			replicationSlotOptions := pglogrepl.CreateReplicationSlotOptions{Mode: pglogrepl.PhysicalReplication}
			_, err = pglogrepl.CreateReplicationSlot(ctx, pgrw.Conn, pgrw.SlotName, "", replicationSlotOptions)
			if err != nil {
				return fmt.Errorf("cannot create replication slot: %w", err)
			}
		} else {
			return fmt.Errorf("cannot get slot information when checking existence: %w", err)
		}
	}

	slotRestartInfo, err = GetSlotInformation(pgrw.Conn, pgrw.SlotName)
	if err != nil {
		return fmt.Errorf("cannot get slot information: %w", err)
	}

	// 3
	sysident, err := pglogrepl.IdentifySystem(ctx, pgrw.Conn)
	if err != nil {
		return fmt.Errorf("cannot identify system: %w", err)
	}

	// 4
	streamStartLSN, streamStartTimeline, err := pgrw.FindStreamingStart()
	if err != nil {
		if !errors.Is(err, ErrNoWalEntries) {
			// just log an error and continue, stream-start-lsn and timeline
			// are required, and we will proceed with slot-info or sysident
			slog.Error("cannot find streaming start", slog.Any("err", err))
		}
	}

	if streamStartLSN == 0 {
		if slotRestartInfo.RestartLSN != 0 {
			streamStartLSN = slotRestartInfo.RestartLSN
			streamStartTimeline = slotRestartInfo.RestartTLI
		}
	}

	if streamStartLSN == 0 {
		streamStartLSN = sysident.XLogPos
		streamStartTimeline = conv.ToUint32(sysident.Timeline)
	}

	// final check
	if streamStartLSN == 0 || streamStartTimeline == 0 {
		return fmt.Errorf("cannot find start LSN for streaming")
	}

	// 5

	// Always start streaming at the beginning of a segment
	curPos := uint64(streamStartLSN) - XLogSegmentOffset(streamStartLSN, walSegSz)
	streamStartLSN = pglogrepl.LSN(curPos)

	slog.Info("start streaming",
		slog.String("lsn", streamStartLSN.String()),
		slog.Uint64("tli", uint64(streamStartTimeline)),
	)

	stream := &StreamCtl{
		StartPos:              streamStartLSN,
		Timeline:              streamStartTimeline,
		StandbyMessageTimeout: 10 * time.Second,
		Synchronous:           true,
		PartialSuffix:         ".partial",
		StreamClient:          pgrw,
		ReplicationSlot:       pgrw.SlotName,
		SysIdentifier:         sysident.SystemID,
		WalSegSz:              walSegSz,
		LastFlushPosition:     pglogrepl.LSN(0),
		StillSending:          true,
		ReportFlushPosition:   true,
		BaseDir:               pgrw.BaseDir,
	}

	err = ReceiveXlogStream(ctx, pgrw.Conn, stream)
	if err != nil {
		slog.Error("stream terminated (ReceiveXlogStream3)", slog.Any("err", err))
	}

	// fsync dir
	err = fsync.FsyncDir(pgrw.BaseDir)
	if err != nil {
		slog.Info("could not finish writing WAL files", slog.Any("err", err))
		// not a fatal error, just log it
		return nil
	}

	if pgrw.Conn != nil {
		err := pgrw.Conn.Close(ctx)
		if err != nil {
			// not a fatal error, just log it
			slog.Info("could not close connection", slog.Any("err", err))
		}
		pgrw.Conn = nil
	}

	return nil
}

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

	err := filepath.WalkDir(pgrw.BaseDir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		base := filepath.Base(path)
		matches := walFileRe.FindStringSubmatch(base)
		if matches == nil {
			return nil // not a WAL file
		}

		xTli, err1 := parseHex32(matches[1])
		xLog, err2 := parseHex32(matches[2])
		xSeg, err3 := parseHex32(matches[3])
		isPartial := matches[4] == ".partial"

		if err1 != nil || err2 != nil || err3 != nil {
			return nil // skip invalid names
		}

		segNo := uint64(xLog)*0x100000000/pgrw.WalSegSz + uint64(xSeg)

		if !isPartial {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("could not stat file %q: %w", path, err)
			}
			if conv.ToUint64(info.Size()) != pgrw.WalSegSz {
				slog.Warn("WAL segment has incorrect size, skipping",
					slog.String("base", base),
					slog.Int64("size", info.Size()),
				)
				return nil
			}
		}

		entries = append(entries, walEntry{
			tli:       xTli,
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
	if segmentFinished {
		slog.Debug("finished segment",
			slog.String("lsn", xlogpos.String()),
			slog.Uint64("tli", uint64(timeline)),
		)
	}

	if pgrw.endpos != 0 && pgrw.endpos < xlogpos {
		slog.Debug("stopped log streaming",
			slog.String("lsn", xlogpos.String()),
			slog.Uint64("tli", uint64(timeline)),
		)
		return true
	}

	if pgrw.prevTimeline != 0 && pgrw.prevTimeline != timeline {
		slog.Debug("switched to timeline",
			slog.String("lsn", pgrw.prevPos.String()),
			slog.Uint64("tli", uint64(timeline)),
		)
	}

	pgrw.prevTimeline = timeline
	pgrw.prevPos = xlogpos

	// Stop if signal channel is closed
	select {
	case <-pgrw.StopCh:
		slog.Debug("received termination signal, stopping streaming")
		return true
	default:
	}

	return false
}
