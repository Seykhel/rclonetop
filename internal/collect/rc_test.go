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
		gotMethod, gotPath = req.Method, req.URL.Path
		body, _ := io.ReadAll(req.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
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
	want := model.JobStats{Bytes: 1234, TotalBytes: 5678, Transfers: 2, TotalTransfers: 5, Checks: 3, TotalChecks: 4, Errors: 1, FatalError: true, Speed: 12.5, Elapsed: 7250 * time.Millisecond, ETA: 4 * time.Second, ETAKnown: true}
	if !reflect.DeepEqual(got.Stats, want) {
		t.Errorf("stats = %+v, want %+v", got.Stats, want)
	}
	if got.Addr != server.URL || got.Source != model.SourceRC || got.At.IsZero() {
		t.Errorf("identity = %+v", got)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestRCConcurrentNoteAndCollect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
