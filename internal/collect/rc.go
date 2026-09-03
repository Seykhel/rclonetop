package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

const rcTimeout = 2 * time.Second
const rcJobTimeout = 500 * time.Millisecond
const maxRCJobs = 256

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
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, addr := range addrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			stats, err := r.stats(ctx, addr)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if stats.Addr != "" {
					snap.RCStats = append(snap.RCStats, stats)
				}
				errs = append(errs, err.Error())
				return
			}
			snap.RCStats = append(snap.RCStats, stats)
		}(addr)
	}
	wg.Wait()
	if len(errs) > 0 {
		if len(snap.RCStats) == 0 {
			return model.Snapshot{}, fmt.Errorf("rc: %s", strings.Join(errs, "; "))
		}
		return snap, fmt.Errorf("rc: %s", strings.Join(errs, "; "))
	}
	return snap, nil
}

type coreStatsResponse struct {
	Bytes          *uint64  `json:"bytes"`
	TotalBytes     *uint64  `json:"totalBytes"`
	Transfers      *int     `json:"transfers"`
	TotalTransfers *int     `json:"totalTransfers"`
	Checks         *int     `json:"checks"`
	TotalChecks    *int     `json:"totalChecks"`
	Errors         *int     `json:"errors"`
	FatalError     *bool    `json:"fatalError"`
	Deletes        *int     `json:"deletes"`
	Renames        *int     `json:"renames"`
	Speed          *float64 `json:"speed"`
	ElapsedTime    *float64 `json:"elapsedTime"`
	ETA            *float64 `json:"eta"`
}

type jobListResponse struct {
	JobIDs *[]int `json:"jobids"`
}

type jobStatusResponse struct {
	Finished  *bool   `json:"finished"`
	Success   *bool   `json:"success"`
	Error     string  `json:"error"`
	Group     string  `json:"group"`
	Duration  float64 `json:"duration"`
	StartTime string  `json:"startTime"`
	EndTime   string  `json:"endTime"`
}

func (r *RC) stats(ctx context.Context, addr string) (model.RCStats, error) {
	url := addr
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	base := strings.TrimRight(url, "/")
	jobCtx, cancelJobs := context.WithTimeout(ctx, rcJobTimeout)
	defer cancelJobs()
	jobResult := make(chan struct {
		jobs []model.RCJob
		err  error
	}, 1)
	go func() {
		jobs, err := r.jobs(jobCtx, base)
		jobResult <- struct {
			jobs []model.RCJob
			err  error
		}{jobs, err}
	}()
	url = base + "/core/stats"
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
	stats := model.RCStats{
		Addr:   addr,
		At:     time.Now(),
		Source: model.SourceRC,
		Stats:  model.JobStats{Source: model.SourceRC},
	}
	if raw.Bytes != nil {
		stats.Stats.Bytes = *raw.Bytes
		stats.Stats.Known |= model.StatsBytes
	}
	if raw.TotalBytes != nil {
		stats.Stats.TotalBytes = *raw.TotalBytes
		stats.Stats.Known |= model.StatsTotalBytes
	}
	if raw.Transfers != nil {
		stats.Stats.Transfers = *raw.Transfers
		stats.Stats.Known |= model.StatsTransfers
	}
	if raw.TotalTransfers != nil {
		stats.Stats.TotalTransfers = *raw.TotalTransfers
		stats.Stats.Known |= model.StatsTotalTransfers
	}
	if raw.Checks != nil {
		stats.Stats.Checks = *raw.Checks
		stats.Stats.Known |= model.StatsChecks
	}
	if raw.TotalChecks != nil {
		stats.Stats.TotalChecks = *raw.TotalChecks
		stats.Stats.Known |= model.StatsTotalChecks
	}
	if raw.Errors != nil {
		stats.Stats.Errors = *raw.Errors
		if raw.FatalError != nil {
			stats.Stats.FatalError = *raw.FatalError
			stats.Stats.Known |= model.StatsFatalError
		}
		stats.Stats.Known |= model.StatsErrors
	}
	if raw.Speed != nil {
		stats.Stats.Speed = *raw.Speed
		stats.Stats.Known |= model.StatsSpeed
	}
	if raw.ElapsedTime != nil {
		stats.Stats.Elapsed = time.Duration(*raw.ElapsedTime * float64(time.Second))
		stats.Stats.Known |= model.StatsElapsed
	}
	if raw.ETA != nil {
		stats.Stats.ETA = time.Duration(*raw.ETA * float64(time.Second))
		stats.Stats.ETAKnown = *raw.ETA >= 0
		if stats.Stats.ETAKnown {
			stats.Stats.Known |= model.StatsETA
		}
	}
	select {
	case result := <-jobResult:
		stats.Jobs = result.jobs
		if result.err != nil {
			return stats, result.err
		}
	case <-jobCtx.Done():
		return stats, fmt.Errorf("%s: job polling: %w", addr, jobCtx.Err())
	}
	return stats, nil
}

func (r *RC) jobs(ctx context.Context, addr string) ([]model.RCJob, error) {
	var listed jobListResponse
	if err := r.postJSON(ctx, addr, "/job/list", "{}", &listed); err != nil {
		return nil, err
	}
	if listed.JobIDs == nil {
		return nil, fmt.Errorf("%s/job/list: invalid response: missing jobids", addr)
	}
	if len(*listed.JobIDs) > maxRCJobs {
		return nil, fmt.Errorf("%s/job/list: too many jobs (%d, maximum %d)", addr, len(*listed.JobIDs), maxRCJobs)
	}
	jobs := make([]model.RCJob, 0, len(*listed.JobIDs))
	results := make([]model.RCJob, len(*listed.JobIDs))
	valid := make([]bool, len(*listed.JobIDs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i, id := range *listed.JobIDs {
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			if id <= 0 {
				mu.Lock()
				errs = append(errs, "invalid job id "+strconv.Itoa(id))
				mu.Unlock()
				return
			}
			var raw jobStatusResponse
			body := `{"jobid":` + strconv.Itoa(id) + `}`
			if err := r.postJSON(ctx, addr, "/job/status", body, &raw); err != nil {
				mu.Lock()
				errs = append(errs, err.Error())
				mu.Unlock()
				return
			}
			if raw.Finished == nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s job %d: invalid response: missing finished", addr, id))
				mu.Unlock()
				return
			}
			job := model.RCJob{ID: id, Group: raw.Group, Finished: *raw.Finished, Error: raw.Error,
				Duration: time.Duration(raw.Duration * float64(time.Second))}
			if raw.Success != nil {
				job.Success, job.SuccessKnown = *raw.Success, true
			}
			var err error
			job.StartTime, err = parseRCTime(raw.StartTime)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s job %d: invalid startTime: %v", addr, id, err))
				mu.Unlock()
				return
			}
			job.EndTime, err = parseRCTime(raw.EndTime)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s job %d: invalid endTime: %v", addr, id, err))
				mu.Unlock()
				return
			}
			mu.Lock()
			results[i], valid[i] = job, true
			mu.Unlock()
		}(i, id)
	}
	wg.Wait()
	for i := range results {
		if valid[i] {
			jobs = append(jobs, results[i])
		}
	}
	if len(errs) > 0 {
		return jobs, fmt.Errorf("%s: %s", addr, strings.Join(errs, "; "))
	}
	return jobs, nil
}

func (r *RC) postJSON(ctx context.Context, addr, path, body string, out any) error {
	url := strings.TrimRight(addr, "/")
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+path, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s%s: %w", addr, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s%s: %w", addr, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s%s: HTTP %s", addr, path, resp.Status)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%s%s: invalid response: %w", addr, path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("%s%s: invalid response: multiple JSON values", addr, path)
		}
		return fmt.Errorf("%s%s: invalid response: trailing data: %w", addr, path, err)
	}
	return nil
}

func parseRCTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}
