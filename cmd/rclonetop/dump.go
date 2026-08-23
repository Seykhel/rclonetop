package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui"
)

// dump runs every collector once and prints the raw result as text.
//
// It exists so a problem can be reported without a screenshot: the output shows
// exactly what each collector saw, including the ones that found nothing, which
// is the first thing worth knowing when the display looks emptier than expected.
func dump(ctx context.Context, w io.Writer, collectors []collect.Collector, base10 bool) error {
	for _, c := range collectors {
		fmt.Fprintf(w, "== %s (source %s, every %s)\n", c.Name(), c.Source(), c.Interval())
		if !c.Available() {
			fmt.Fprintln(w, "   unavailable on this host")
			continue
		}

		// Two passes with a pause between them: rates are derived from the
		// delta between samples, so a single collection can only ever report
		// zero.
		snap, err := c.Collect(ctx)
		if err != nil {
			fmt.Fprintf(w, "   error: %v\n", err)
			continue
		}
		time.Sleep(500 * time.Millisecond)
		snap, err = c.Collect(ctx)
		if err != nil {
			fmt.Fprintf(w, "   error: %v\n", err)
			continue
		}

		if len(snap.Processes)+len(snap.Mounts)+len(snap.Caches)+
			len(snap.SyncPairs)+len(snap.Units)+len(snap.Jobs) == 0 {
			fmt.Fprintln(w, "   nothing found")
		}
		for _, mnt := range snap.Mounts {
			fmt.Fprintf(w, "   mount %s → %q (%s)\n", mnt.Remote, mnt.Mountpoint, mnt.FSType)
		}
		for _, c := range snap.Caches {
			fmt.Fprintf(w, "   cache %s  %s in %d files  %q\n",
				c.Kind, ui.Bytes(c.Bytes, base10), c.Files, c.Path)
		}
		for _, u := range snap.Units {
			fmt.Fprintf(w, "   unit %s (%s)  %s/%s  result=%s status=%d\n",
				u.Name, u.Scope, u.ActiveState, u.SubState, u.Result, u.ExitStatus)
			if !u.ActiveEnter.IsZero() || !u.InactiveEnter.IsZero() {
				fmt.Fprintf(w, "      active since %s  inactive since %s\n",
					stamp(u.ActiveEnter), stamp(u.InactiveEnter))
			}
			if u.IsTimer() {
				fmt.Fprintf(w, "      triggers %s  last %s  next %s\n",
					u.Triggers, stamp(u.LastTrigger), stamp(u.NextElapse))
			}
			for _, e := range u.Errors {
				fmt.Fprintf(w, "      [%d] %s %s\n", e.Priority, stamp(e.At), e.Message)
			}
		}
		for _, j := range snap.Jobs {
			fmt.Fprintf(w, "   job %s pid %d  %q\n", j.Kind, j.PID, j.LogFile)
			if j.ReadError != "" {
				fmt.Fprintf(w, "      unreadable: %s\n", j.ReadError)
			}
			if j.Path1 != "" || j.Path2 != "" {
				fmt.Fprintf(w, "      paths %q → %q\n", j.Path1, j.Path2)
			}
			// "no statistics yet" and "nothing transferred" are different
			// answers, and only one of them is a measurement.
			if j.HaveStats {
				s := j.Stats
				fmt.Fprintf(w, "      %s / %s in %d/%d files  checks %d/%d  errors %d (fatal %v)\n",
					ui.Bytes(s.Bytes, base10), ui.Bytes(s.TotalBytes, base10),
					s.Transfers, s.TotalTransfers, s.Checks, s.TotalChecks,
					s.Errors, s.FatalError)
				fmt.Fprintf(w, "      speed %s  elapsed %s  eta %s\n",
					ui.Rate(s.Speed, base10), ui.Duration(s.Elapsed), eta(s))
			} else {
				fmt.Fprintln(w, "      no statistics block read yet")
			}
			fmt.Fprintf(w, "      last line %s  outcome %q (finished %v)\n",
				stamp(j.At), j.Outcome, j.Finished)
			for _, e := range j.Errors {
				fmt.Fprintf(w, "      [%d] %s %s\n", e.Priority, stamp(e.At), e.Message)
			}
		}
		for _, p := range snap.SyncPairs {
			fmt.Fprintf(w, "   sync %q\n", p.Name)
			fmt.Fprintf(w, "      left  %-28s %6d files  %s  %s\n",
				p.Left.Label, p.Left.Files, ui.Bytes(p.Left.Bytes, base10), p.Left.Path)
			fmt.Fprintf(w, "      right %-28s %6d files  %s  %s\n",
				p.Right.Label, p.Right.Files, ui.Bytes(p.Right.Bytes, base10), p.Right.Path)
			fmt.Fprintf(w, "      drift %d  listed %s  failed %s\n",
				p.Drift, stamp(p.ListedAt), stamp(p.FailedAt))
		}
		for _, p := range snap.Processes {
			fmt.Fprintf(w, "   pid %d  kind %s  remotes %v  target %q\n",
				p.PID, p.Kind, p.Remotes, p.Target)
			fmt.Fprintf(w, "      up %s  rss %s  threads %d  rc %q\n",
				ui.Duration(p.Uptime()), ui.Bytes(p.RSS, base10), p.Threads, p.RCAddr)
			fmt.Fprintf(w, "      io %v  read %s (%s)  write %s (%s)\n",
				p.IOAvailable,
				ui.Bytes(p.ReadTotal, base10), ui.Rate(p.ReadRate, base10),
				ui.Bytes(p.WriteTotal, base10), ui.Rate(p.WriteRate, base10))
			fmt.Fprintf(w, "      args %s\n", strings.Join(p.Args, " "))
		}
	}
	return nil
}

// eta formats an estimate, keeping "rclone cannot say" distinct from "no time
// left at all".
func eta(s model.JobStats) string {
	if !s.ETAKnown {
		return "unknown"
	}
	return ui.Duration(s.ETA)
}

// stamp formats a timestamp for the dump, distinguishing "never" from a real
// instant so an empty field is not read as the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}
