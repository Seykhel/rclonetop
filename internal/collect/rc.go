package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

const rcTimeout = 2 * time.Second

// RC reads rclone's read-only remote-control API from addresses already found
// in the process table. It never guesses an address and never scans the host.
type RC struct {
	mu     sync.Mutex
	addrs  map[string]bool
	client *http.Client
}

// NewRC returns an RC collector using bounded HTTP requests.
func NewRC() *RC {
	return NewRCWith(&http.Client{Timeout: rcTimeout})
}

// NewRCWith is the test seam for the HTTP transport.
func NewRCWith(client *http.Client) *RC {
	if client == nil {
		client = &http.Client{Timeout: rcTimeout}
	}
	bounded := *client
	// An RC address came from a local process command line. Following a
	// redirect would turn that fact into an unsolicited request elsewhere.
	bounded.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &RC{addrs: make(map[string]bool), client: &bounded}
}

func (r *RC) Name() string            { return "rc" }
func (r *RC) Source() model.Source    { return model.SourceRC }
func (r *RC) Interval() time.Duration { return 2 * time.Second }

// Available is always true because the endpoint is learned after the first
// process sample. collect.Run checks availability only once at startup.
func (r *RC) Available() bool { return true }

// NoteProcesses replaces the known endpoint set with the current process set.
func (r *RC) NoteProcesses(processes []model.Process) {
	addrs := make(map[string]bool)
	for _, p := range processes {
		if p.RCAddr != "" {
			addrs[p.RCAddr] = true
		}
	}
	r.mu.Lock()
	r.addrs = addrs
	r.mu.Unlock()
}

func (r *RC) Collect(ctx context.Context) (model.Snapshot, error) {
	r.mu.Lock()
	addrs := make([]string, 0, len(r.addrs))
	for addr := range r.addrs {
		addrs = append(addrs, addr)
	}
	r.mu.Unlock()

	snap := model.Snapshot{
		At:      time.Now(),
		Source:  model.SourceRC,
		RCStats: make([]model.RCStats, 0, len(addrs)),
	}
	var errs []string
	for _, addr := range addrs {
		stats, err := r.stats(ctx, addr)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		snap.RCStats = append(snap.RCStats, stats)
	}
	if len(errs) > 0 {
		if len(snap.RCStats) == 0 {
			return model.Snapshot{}, fmt.Errorf("rc: %s", strings.Join(errs, "; "))
		}
		return snap, fmt.Errorf("rc: %s", strings.Join(errs, "; "))
	}
	return snap, nil
}

type coreStatsResponse struct {
	Bytes          uint64   `json:"bytes"`
	TotalBytes     uint64   `json:"totalBytes"`
	Transfers      int      `json:"transfers"`
	TotalTransfers int      `json:"totalTransfers"`
	Checks         int      `json:"checks"`
	TotalChecks    int      `json:"totalChecks"`
	Errors         int      `json:"errors"`
	FatalError     bool     `json:"fatalError"`
	Deletes        int      `json:"deletes"`
	Renames        int      `json:"renames"`
	Speed          float64  `json:"speed"`
	ElapsedTime    float64  `json:"elapsedTime"`
	ETA            *float64 `json:"eta"`
}

func (r *RC) stats(ctx context.Context, addr string) (model.RCStats, error) {
	url := addr
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	url = strings.TrimRight(url, "/") + "/core/stats"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return model.RCStats{}, fmt.Errorf("%s: %w", addr, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return model.RCStats{}, fmt.Errorf("%s: %w", addr, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return model.RCStats{}, fmt.Errorf("%s: HTTP %s", addr, resp.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	var raw coreStatsResponse
	if err := decoder.Decode(&raw); err != nil {
		return model.RCStats{}, fmt.Errorf("%s: invalid core/stats response: %w", addr, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return model.RCStats{}, fmt.Errorf("%s: invalid core/stats response: multiple JSON values", addr)
		}
		return model.RCStats{}, fmt.Errorf("%s: invalid core/stats response: trailing data: %w", addr, err)
	}
	return model.RCStats{
		Addr:   addr,
		At:     time.Now(),
		Source: model.SourceRC,
		Stats: model.JobStats{
			Bytes: raw.Bytes, TotalBytes: raw.TotalBytes,
			Transfers: raw.Transfers, TotalTransfers: raw.TotalTransfers,
			Checks: raw.Checks, TotalChecks: raw.TotalChecks,
			Errors: raw.Errors, FatalError: raw.FatalError,
			Deletes: raw.Deletes, Renames: raw.Renames,
			Speed:   raw.Speed,
			Elapsed: time.Duration(raw.ElapsedTime * float64(time.Second)),
			ETA: func() time.Duration {
				if raw.ETA == nil {
					return 0
				}
				return time.Duration(*raw.ETA * float64(time.Second))
			}(),
			ETAKnown: raw.ETA != nil && *raw.ETA >= 0,
		},
	}, nil
}
