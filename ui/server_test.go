package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeClient struct{ snap Snapshot }

func (f *fakeClient) Snapshot(context.Context, Receiver) Snapshot { return f.snap }

func TestFilterWALByQueryAndExtension(t *testing.T) {
	files := []WALFile{
		{Name: "0001", Filename: "0001.zst", Ext: "zst"},
		{Name: "0002", Filename: "0002.gz", Ext: "gz"},
		{Name: "0003", Filename: "0003", Ext: ""},
	}

	got := filterWAL(files, "0002", "all")
	if len(got) != 1 || got[0].Filename != "0002.gz" {
		t.Fatalf("query filter mismatch: %#v", got)
	}

	got = filterWAL(files, "", "plain")
	if len(got) != 1 || got[0].Filename != "0003" {
		t.Fatalf("plain filter mismatch: %#v", got)
	}

	got = filterWAL(files, "", "zst")
	if len(got) != 1 || got[0].Filename != "0001.zst" {
		t.Fatalf("zst filter mismatch: %#v", got)
	}
}

func TestStatusPageRenders(t *testing.T) {
	server := NewServer(Options{
		Receivers: []Receiver{{Label: "local", Addr: "http://127.0.0.1:7070"}},
		Client: &fakeClient{snap: Snapshot{
			Receiver: Receiver{Label: "local", Addr: "http://127.0.0.1:7070"},
			Status:   &PgrwlStatus{RunningMode: "receive", StreamStatus: &StreamStatus{Slot: "pgrwl", Timeline: 1, LastFlushLSN: "0/16B6C50", Uptime: "1h", Running: true}},
			Config:   &BriefConfig{RetentionEnable: true},
		}},
	})

	mux := http.NewServeMux()
	server.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/ui/status", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status code = %d", res.Code)
	}
	body := res.Body.String()
	for _, want := range []string{"pgrwl", "Restore Readiness", "0/16B6C50", "Retention"} {
		if !strings.Contains(body, want) {
			t.Fatalf("response does not contain %q\n%s", want, body)
		}
	}
}

func TestRestoreReadinessDetectsCoveringWALAndSequence(t *testing.T) {
	started := time.Date(2026, 4, 25, 2, 0, 0, 0, time.UTC)
	files := []WALFile{
		{Name: "0000000100000000000000AB", Filename: "0000000100000000000000AB.gz", Ext: "gz"},
		{Name: "0000000100000000000000AC", Filename: "0000000100000000000000AC.gz", Ext: "gz"},
		{Name: "0000000100000000000000AD", Filename: "0000000100000000000000AD.gz", Ext: "gz"},
	}

	got := restoreReadiness(View{Snapshot: Snapshot{
		Status:   &PgrwlStatus{StreamStatus: &StreamStatus{LastFlushLSN: "0/16B6C50"}},
		WALFiles: files,
		Backups: []Backup{{
			Label:      "base_20260425_020001",
			Started:    started,
			Finished:   started.Add(3 * time.Minute),
			WALStopLSN: "0/AB000000",
			Status:     "done",
		}},
	}})

	if got.PossibleLabel != "yes" {
		t.Fatalf("restore possible = %q, want yes: %#v", got.PossibleLabel, got)
	}
	if got.CoveringWAL != "0000000100000000000000AB" {
		t.Fatalf("covering wal = %q", got.CoveringWAL)
	}
	if got.SequenceLabel != "correct" {
		t.Fatalf("sequence label = %q", got.SequenceLabel)
	}
}

func TestRestoreReadinessDetectsGapAfterCoveringWAL(t *testing.T) {
	started := time.Date(2026, 4, 25, 2, 0, 0, 0, time.UTC)
	files := []WALFile{
		{Name: "0000000100000000000000AB", Filename: "0000000100000000000000AB.gz", Ext: "gz"},
		{Name: "0000000100000000000000AD", Filename: "0000000100000000000000AD.gz", Ext: "gz"},
	}

	got := restoreReadiness(View{Snapshot: Snapshot{
		Status:   &PgrwlStatus{StreamStatus: &StreamStatus{LastFlushLSN: "0/16B6C50"}},
		WALFiles: files,
		Backups: []Backup{{
			Label:      "base_20260425_020001",
			Started:    started,
			Finished:   started.Add(3 * time.Minute),
			WALStopLSN: "0/AB000000",
			Status:     "done",
		}},
	}})

	if got.PossibleLabel != "no" {
		t.Fatalf("restore possible = %q, want no: %#v", got.PossibleLabel, got)
	}
	if got.SequenceLabel != "gap after 00AB" {
		t.Fatalf("sequence label = %q", got.SequenceLabel)
	}
}

func TestWALTableFragmentRendersFilteredRows(t *testing.T) {
	server := NewServer(Options{
		Receivers: []Receiver{{Label: "local", Addr: "http://127.0.0.1:7070"}},
		Client: &fakeClient{snap: Snapshot{
			Receiver: Receiver{Label: "local", Addr: "http://127.0.0.1:7070"},
			WALFiles: []WALFile{
				{Name: "0001", Filename: "0001.zst", Ext: "zst", SizeMB: 16.0, Encrypted: true},
				{Name: "0002", Filename: "0002.gz", Ext: "gz", SizeMB: 16.0},
			},
		}},
	})

	mux := http.NewServeMux()
	server.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/ui/fragments/wal-table?ext=zst", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	body := res.Body.String()
	if !strings.Contains(body, "0001") || strings.Contains(body, "0002") {
		t.Fatalf("unexpected filtered table:\n%s", body)
	}
}
