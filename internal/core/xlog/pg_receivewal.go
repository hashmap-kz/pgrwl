package xlog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/hashmap-kz/pgrwl/internal/core/conv"
	"github.com/hashmap-kz/pgrwl/internal/core/coreutils"
	"github.com/hashmap-kz/pgrwl/internal/core/fsync"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jackc/pglogrepl"
)

type PgReceiveWal struct {
	BaseDir     string
	WalSegSz    uint64
	Conn        *pgconn.PgConn
	ConnStrRepl string
	SlotName    string
	Verbose     bool

	streamMu sync.RWMutex
	stream   *StreamCtl // current active stream (or nil)
}

var ErrNoWalEntries = fmt.Errorf("no valid WAL segments found")

func NewPgReceiver(ctx context.Context, opts *coreutils.Opts) (*PgReceiveWal, error) {
	connStrRepl := fmt.Sprintf("application_name=%s replication=yes", opts.Slot)
	conn, err := pgconn.Connect(ctx, connStrRepl)
	if err != nil {
		slog.Error("cannot establish connection", slog.Any("err", err))
		return nil, err
	}
	startupInfo, err := GetStartupInfo(conn)
	if err != nil {
		return nil, err
	}
	return &PgReceiveWal{
		BaseDir:     opts.Directory,
		WalSegSz:    startupInfo.WalSegSz,
		Conn:        conn,
		ConnStrRepl: connStrRepl,
		SlotName:    opts.Slot,
		// To prevent log-attributes evaluation, and fully eliminate function calls for non-trace levels
		Verbose: strings.EqualFold(os.Getenv("LOG_LEVEL"), "trace"),
	}, nil
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
	streamStartLSN, streamStartTimeline, err := pgrw.findStreamingStart()
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

	slog.Info("starting log streaming",
		slog.String("lsn", streamStartLSN.String()),
		slog.Uint64("tli", uint64(streamStartTimeline)),
	)

	stream := NewStream(&StreamOpts{
		StartPos:        streamStartLSN,
		Timeline:        streamStartTimeline,
		ReplicationSlot: pgrw.SlotName,
		WalSegSz:        pgrw.WalSegSz,
		BaseDir:         pgrw.BaseDir,
		Conn:            pgrw.Conn,
		Verbose:         pgrw.Verbose,
	})
	pgrw.SetStream(stream)

	err = stream.ReceiveXlogStream(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("log streaming terminated: context canceled")
		} else {
			slog.Error("log streaming terminated", slog.Any("err", err))
		}
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

func (pgrw *PgReceiveWal) SetStream(s *StreamCtl) {
	pgrw.streamMu.Lock()
	defer pgrw.streamMu.Unlock()
	pgrw.stream = s
}

// findStreamingStart scans baseDir for WAL files and returns (startLSN, timeline)
func (pgrw *PgReceiveWal) findStreamingStart() (pglogrepl.LSN, uint32, error) {
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

		isPartial := IsPartialXLogFileName(base)
		if !IsXLogFileName(base) && !isPartial {
			return nil
		}

		tli, segNo, err := XLogFromFileName(base, pgrw.WalSegSz)
		if err != nil {
			return err
		}

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
		startLSN = XLogSegNoToRecPtr(best.segNo, pgrw.WalSegSz)
	} else {
		startLSN = XLogSegNoToRecPtr(best.segNo+1, pgrw.WalSegSz)
	}

	slog.Debug("found streaming start (based on WAL dir)",
		slog.String("lsn", startLSN.String()),
		slog.Uint64("tli", uint64(best.tli)),
		slog.String("wal", best.basename),
	)
	return startLSN, best.tli, nil
}
