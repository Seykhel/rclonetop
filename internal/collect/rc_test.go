package collect

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

func TestRCCollectsCoreStatsFromObservedEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/core/stats" {
			gotMethod, gotPath = req.Method, req.URL.Path
		}
		body, _ := io.ReadAll(req.Body)
		if req.URL.Path == "/core/stats" {
			gotBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"bytes":1234,"totalBytes":5678,"transfers":2,"totalTransfers":5,"checks":3,"totalChecks":4,"errors":1,"fatalError":true,"speed":12.5,"elapsedTime":7.25,"eta":4}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/core/stats" || gotBody != "{}" {
		t.Fatalf("request = %s %s %q, want POST /core/stats {}", gotMethod, gotPath, gotBody)
	}
	if len(snap.RCStats) != 1 {
		t.Fatalf("got %d RC stats, want one", len(snap.RCStats))
	}
	got := snap.RCStats[0]
	want := model.JobStats{Known: model.StatsBytes | model.StatsTotalBytes | model.StatsTransfers | model.StatsTotalTransfers | model.StatsChecks | model.StatsTotalChecks | model.StatsErrors | model.StatsFatalError | model.StatsSpeed | model.StatsElapsed | model.StatsETA, Source: model.SourceRC, Bytes: 1234, TotalBytes: 5678, Transfers: 2, TotalTransfers: 5, Checks: 3, TotalChecks: 4, Errors: 1, FatalError: true, Speed: 12.5, Elapsed: 7250 * time.Millisecond, ETA: 4 * time.Second, ETAKnown: true}
	if !reflect.DeepEqual(got.Stats, want) {
		t.Errorf("stats = %+v, want %+v", got.Stats, want)
	}
	if got.Addr != server.URL || got.Source != model.SourceRC || got.At.IsZero() {
		t.Errorf("identity = %+v", got)
	}
}

// core/stats answers with the same "transferring" shape the log's JSON stats
// blocks carry, and a process with no on-disk log -- the common case for a
// long-lived mount, which nothing tells to write one -- has no other way to
// say which files are in flight.
func TestRCCollectsTransferringFromCoreStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"transferring":[{"name":"b.txt","bytes":200,"size":400,"percentage":50,"speed":9.0},{"name":"a.txt","bytes":100,"size":100,"percentage":100,"speed":5.0,"eta":0}]}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.RCStats) != 1 {
		t.Fatalf("got %d RC stats, want one", len(snap.RCStats))
	}

	// sortTransfers puts them by name, which is also the point: the object
	// comes out of a Go map on rclone's side and arrives shuffled.
	want := []model.Transfer{
		{Name: "a.txt", Bytes: 100, BytesKnown: true, Size: 100, Percentage: 100, Speed: 5.0, ETA: 0, ETAKnown: true},
		{Name: "b.txt", Bytes: 200, BytesKnown: true, Size: 400, Percentage: 50, Speed: 9.0},
	}
	if got := snap.RCStats[0].Transferring; !reflect.DeepEqual(got, want) {
		t.Errorf("transferring = %+v, want %+v", got, want)
	}
}

// The distinction mirrors Job.Transferring's: nil says core/stats has not
// named a "transferring" key at all -- an rclone too old to send one, say --
// and must not be read as "asked, and it counted zero". An empty answer is a
// measurement and stays non-nil.
func TestRCTransferringIsNilOnlyWhenTheKeyIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"bytes":0}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := snap.RCStats[0].Transferring; got != nil {
		t.Errorf("transferring = %+v, want nil: the response named no such key", got)
	}
}

func TestRCMeasuredEmptyTransferringIsNotNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"transferring":[]}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := snap.RCStats[0].Transferring
	if got == nil || len(got) != 0 {
		t.Errorf("transferring = %+v, want a measured empty list", got)
	}
}

// A transfer record can arrive with fields missing -- an older daemon, or a
// response the endpoint populated only in part. Every absent measurement must
// stay unknown: a zero would claim an empty file that has not started moving,
// which is the opposite of what a daemon that did not report them is saying. A
// negative eta is rclone declining to estimate, the same as a missing one.
func TestRCPreservesUnknownTransferringFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"transferring":[{"name":"bare.bin"},{"name":"stalled.bin","percentage":10,"size":100,"eta":-1}]}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.RCStats) != 1 {
		t.Fatalf("got %d RC stats, want one", len(snap.RCStats))
	}

	got := snap.RCStats[0].Transferring
	want := []model.Transfer{
		{Name: "bare.bin", Percentage: model.UnknownPercentage, Size: model.UnknownSize, Speed: model.UnknownSpeed},
		{Name: "stalled.bin", Percentage: 10, Size: 100, Speed: model.UnknownSpeed},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transferring = %+v, want %+v", got, want)
	}
	if got[0].BytesKnown {
		t.Errorf("absent bytes were reported as a measured zero: %+v", got[0])
	}
	if got[0].ETAKnown || got[1].ETAKnown {
		t.Errorf("absent or negative eta was reported as an estimate: %+v", got)
	}
}

// A transferring entry whose fields are the wrong type is malformed input, and
// malformed input fails the whole core/stats read rather than being silently
// coerced: the daemon is not speaking the protocol this parser expects, and a
// half-understood sample is worse than none.
func TestRCRejectsMalformedTransferringEntry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"transferring":[{"name":"x","size":"big"}]}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err == nil || len(snap.RCStats) != 0 {
		t.Fatalf("malformed transferring was not rejected: snap=%+v err=%v", snap, err)
	}
}

func TestRCCollectsAsyncJobStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/core/stats":
			_, _ = w.Write([]byte(`{}`))
		case "/job/list":
			_, _ = w.Write([]byte(`{"jobids":[1,2,3]}`))
		case "/job/status":
			body, _ := io.ReadAll(req.Body)
			switch string(body) {
			case `{"jobid":1}`:
				_, _ = w.Write([]byte(`{"finished":false,"group":"backup","duration":1.5}`))
			case `{"jobid":2}`:
				_, _ = w.Write([]byte(`{"finished":true,"success":true,"duration":4,"startTime":"2026-09-03T10:00:00Z","endTime":"2026-09-03T10:00:04Z"}`))
			case `{"jobid":3}`:
				_, _ = w.Write([]byte(`{"finished":true,"success":false,"error":"remote failed"}`))
			}
		}
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	jobs := snap.RCStats[0].Jobs
	if len(jobs) != 3 || jobs[0].ID != 1 || jobs[0].Finished || jobs[1].ID != 2 || !jobs[1].Success || jobs[2].Success || jobs[2].Error != "remote failed" {
		t.Fatalf("jobs = %+v", jobs)
	}
	if !jobs[1].SuccessKnown || !jobs[1].StartTime.Equal(time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("completed job fields = %+v", jobs[1])
	}
}

func TestRCDoesNotProbeBeforeProcessesAreObserved(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { called = true }))
	defer server.Close()

	rc := NewRCWith(server.Client())
	snap, err := rc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if called || snap.RCStats == nil {
		t.Fatalf("collector probed without an observed endpoint: called=%v stats=%v", called, snap.RCStats)
	}
}

func TestRCReportsEndpointFailure(t *testing.T) {
	rc := NewRCWith(&http.Client{Timeout: time.Second})
	rc.NoteProcesses([]model.Process{{RCAddr: "127.0.0.1:1"}})
	if _, err := rc.Collect(context.Background()); err == nil {
		t.Fatal("Collect succeeded for an unreachable endpoint")
	}
}

func TestRCPreservesSuccessfulStatsWhenAnotherEndpointFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"bytes":99}`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}, {RCAddr: "127.0.0.1:1"}})
	snap, err := rc.Collect(context.Background())
	if err == nil {
		t.Fatal("Collect succeeded despite one unreachable endpoint")
	}
	if len(snap.RCStats) != 1 || snap.RCStats[0].Stats.Bytes != 99 {
		t.Fatalf("successful endpoint data was lost: %+v", snap.RCStats)
	}
}

func TestRCRejectsMalformedCoreStatsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	if _, err := rc.Collect(context.Background()); err == nil {
		t.Fatal("Collect accepted malformed core/stats JSON")
	}
}

func TestRCRejectsTrailingCoreStatsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"bytes":1} trailing`))
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	if _, err := rc.Collect(context.Background()); err == nil {
		t.Fatal("Collect accepted trailing core/stats data")
	}
}

func TestRCRejectsMalformedJobStatusResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/core/stats":
			_, _ = w.Write([]byte(`{}`))
		case "/job/list":
			_, _ = w.Write([]byte(`{"jobids":[7]}`))
		case "/job/status":
			_, _ = w.Write([]byte(`not json`))
		}
	}))
	defer server.Close()

	rc := NewRCWith(server.Client())
	rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
	snap, err := rc.Collect(context.Background())
	if err == nil || len(snap.RCStats) != 1 || len(snap.RCStats[0].Jobs) != 0 {
		t.Fatalf("malformed job response was not isolated: snap=%+v err=%v", snap, err)
	}
}

func TestRCConcurrentNoteAndCollect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/job/list" {
			_, _ = w.Write([]byte(`{"jobids":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"bytes":1}`))
	}))
	defer server.Close()
	rc := NewRCWith(server.Client())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			rc.NoteProcesses([]model.Process{{RCAddr: server.URL}})
		}()
		go func() {
			defer wg.Done()
			_, _ = rc.Collect(context.Background())
		}()
	}
	wg.Wait()
}
