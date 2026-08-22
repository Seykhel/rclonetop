package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Seykhel/rclonetop/internal/collect"
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

		if len(snap.Processes) == 0 {
			fmt.Fprintln(w, "   no rclone processes found")
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
