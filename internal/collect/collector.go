// Package collect turns the many ways rclone can be observed into one stream
// of snapshots.
//
// The design constraint that shapes this package: most rclone installations do
// not run the rc API. rclone is typically driven by cron or systemd timers, or
// left running as a FUSE mount, none of which expose anything to talk to. A
// monitor that only speaks rc shows an empty screen on those hosts, so every
// source of truth gets its own collector and the UI renders whatever is
// actually available.
package collect

import (
	"context"
	"sync"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

// Collector is one source of truth about rclone activity.
type Collector interface {
	// Name is the short identifier used in logs and in the UI.
	Name() string

	// Source is the tag stamped onto every snapshot this collector emits.
	Source() model.Source

	// Interval is how often this collector wants to run. It is per-collector
	// because the costs differ by orders of magnitude: reading /proc is
	// free and can run every second, while walking a cache directory is not
	// and must not.
	Interval() time.Duration

	// Available reports whether this collector can produce anything on this
	// host. When it is false the UI hides the corresponding section instead
	// of rendering zeros, because a zero and a missing measurement mean very
	// different things to someone checking whether their backup ran.
	Available() bool

	// Collect takes one observation. It must be safe to call from its own
	// goroutine and must respect ctx cancellation.
	Collect(ctx context.Context) (model.Snapshot, error)
}

// Result is one collection attempt, successful or not.
type Result struct {
	Name     string
	Source   model.Source
	Snapshot model.Snapshot
	Err      error
}

// Run starts each collector on its own ticker and funnels every result into a
// single channel. The channel closes once ctx is cancelled and all collectors
// have stopped.
//
// Each collector runs in its own goroutine so a slow source cannot stall a fast
// one: a directory walk that takes two seconds must never delay the bandwidth
// graph.
func Run(ctx context.Context, collectors []Collector) <-chan Result {
	out := make(chan Result)

	var wg sync.WaitGroup
	for _, c := range collectors {
		if !c.Available() {
			continue
		}
		wg.Add(1)
		go func(c Collector) {
			defer wg.Done()
			runOne(ctx, c, out)
		}(c)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

// runOne drives a single collector until ctx is cancelled. It collects once
// immediately so the first frame is not empty, then settles into its interval.
func runOne(ctx context.Context, c Collector, out chan<- Result) {
	ticker := time.NewTicker(c.Interval())
	defer ticker.Stop()

	for {
		snap, err := c.Collect(ctx)
		res := Result{Name: c.Name(), Source: c.Source(), Snapshot: snap, Err: err}

		select {
		case out <- res:
		case <-ctx.Done():
			return
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
