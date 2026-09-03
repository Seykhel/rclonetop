package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// dump runs every collector once and prints the raw result as text.
//
// It exists so a problem can be reported without a screenshot: the output shows
// exactly what each collector saw, including the ones that found nothing, which
// is the first thing worth knowing when the display looks emptier than expected.
func dump(ctx context.Context, w io.Writer, collectors []collect.Collector, base10 bool, d display) error {
	writeDisplay(w, d)

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
			// Some collectors retain useful partial data alongside an error.
			// Keep that data in the diagnostic instead of hiding it behind the
			// error line.
			if snap.Source == "" {
				continue
			}
		} else {
			time.Sleep(500 * time.Millisecond)
			snap, err = c.Collect(ctx)
			if err != nil {
				fmt.Fprintf(w, "   error: %v\n", err)
				if snap.Source == "" {
					continue
				}
			}
		}

		if len(snap.Processes)+len(snap.Mounts)+len(snap.Caches)+
			len(snap.SyncPairs)+len(snap.Units)+len(snap.Jobs)+len(snap.RCStats) == 0 {
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
			if u.LogFile != "" {
				fmt.Fprintf(w, "      log %q\n", u.LogFile)
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
			kind := string(j.Kind)
			if kind == "" {
				// Nothing has said what this job is: no process to read a
				// command line from, and the log only says so for bisync.
				kind = "-"
			}
			fmt.Fprintf(w, "   job %s pid %d  %q\n", kind, j.PID, j.LogFile)
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
					ui.Rate(s.Speed, base10), ui.Duration(s.Elapsed), eta(s.ETA, s.ETAKnown))
			} else {
				fmt.Fprintln(w, "      no statistics block read yet")
			}
			// Nil and empty are different answers here: "no block has named the
			// files" against "the block named none", and only the second says
			// the run is between files rather than unobserved.
			switch {
			case j.Transferring == nil:
				fmt.Fprintln(w, "      files in flight not reported by this log")
			case len(j.Transferring) == 0:
				fmt.Fprintln(w, "      no file in flight")
			}
			for _, t := range j.Transferring {
				fmt.Fprintf(w, "      → %s  %s of %s  %s  eta %s\n",
					t.Name, transferDone(t, base10), transferSize(t.Size, base10),
					ui.Rate(t.Speed, base10), eta(t.ETA, t.ETAKnown))
			}
			fmt.Fprintf(w, "      last line %s  outcome %q (finished %v)\n",
				stamp(j.At), j.Outcome, j.Finished)
			for _, e := range j.Errors {
				fmt.Fprintf(w, "      [%d] %s %s\n", e.Priority, stamp(e.At), e.Message)
			}
		}
		for _, r := range snap.RCStats {
			s := r.Stats
			fmt.Fprintf(w, "   rc %q  %s / %s in %d/%d files  checks %d/%d  errors %d (fatal %v)\n",
				r.Addr, ui.Bytes(s.Bytes, base10), ui.Bytes(s.TotalBytes, base10),
				s.Transfers, s.TotalTransfers, s.Checks, s.TotalChecks, s.Errors, s.FatalError)
			fmt.Fprintf(w, "      speed %s  elapsed %s  eta %s\n",
				ui.Rate(s.Speed, base10), ui.Duration(s.Elapsed), eta(s.ETA, s.ETAKnown))
			for _, j := range r.Jobs {
				state := "running"
				if j.Finished {
					state = "finished"
					if j.SuccessKnown {
						state = "successful"
						if !j.Success {
							state = "failed"
						}
					}
				}
				fmt.Fprintf(w, "      async job %d  %s  error=%q\n", j.ID, state, j.Error)
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

// display is what -d reports about how the screen would have been coloured.
//
// "The colours look washed out" is the one complaint a collector dump cannot
// answer, because it is not about what was collected. It has three candidate
// causes -- the terminal cannot do 24-bit colour, rclonetop was told to pretend
// it cannot, or the theme is not the one the user thinks -- and they are
// indistinguishable from a screenshot. All three are printed here instead.
type display struct {
	// Profile is what termenv detected, after -t and -l have had their say. It
	// is read from lipgloss rather than the flags, so it reports what the
	// gradients were actually quantised to and not what was asked for.
	Profile termenv.Profile
	Theme   string
	Symbol  graph.Symbol

	// Term and ColorTerm are the two variables that decide the profile. The
	// investigation behind this needed exactly them: tmux advertising *:RGB and
	// propagating COLORTERM is what ruled colour depth out as the cause, and
	// without them in the dump that was an argument rather than a reading.
	Term, ColorTerm string

	// Terminal says whether stdout is one, and it has to be printed because of
	// how this output is used. termenv reports Ascii for a pipe, correctly --
	// and users are asked to paste -d into a bug report, which means they run it
	// through a pipe or a redirection every single time. Reporting "no colour"
	// to somebody whose complaint is that the colours look wrong would answer
	// the wrong question with total confidence, so when this is false the two
	// environment variables above are the reading that matters and the profile
	// line is about the pipe.
	Terminal bool
}

func writeDisplay(w io.Writer, d display) {
	fmt.Fprintln(w, "== display")
	fmt.Fprintf(w, "   colour profile %s\n", profileName(d.Profile))
	if !d.Terminal {
		fmt.Fprintln(w, "   (stdout is not a terminal, so that is the pipe's profile, not the screen's;")
		fmt.Fprintln(w, "    TERM and COLORTERM below are what the real terminal advertises)")
	}
	fmt.Fprintf(w, "   theme %q  graph symbol %s\n", d.Theme, symbolName(d.Symbol))
	fmt.Fprintf(w, "   TERM=%q COLORTERM=%q\n", d.Term, d.ColorTerm)
}

// isTerminal reports whether f is a terminal rather than a pipe or a file.
//
// This asked the mode bits first -- a character device -- on the reasoning that
// one bit did not justify a dependency. The reasoning was sound and the premise
// was false: /dev/null is a character device too, so `-d > /dev/null` reported a
// terminal and suppressed the very caveat the answer exists to carry.
//
// term.IsTerminal is a real ioctl(TCGETS), which fails with ENOTTY on anything
// that is not a tty, /dev/null included. It costs nothing to reach for: x/term
// is already in the module graph and already linked into the binary, because
// bubbletea uses it to take the alternate screen. Only go.mod changes, and only
// to admit a dependency that was there all along.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(f.Fd())
}

// symbolName spells out the unchosen case rather than printing an empty string,
// which reads as a missing value instead of as the third state it is.
func symbolName(s graph.Symbol) string {
	if s == "" {
		return fmt.Sprintf("%q (nobody chose one; this is the default)", graph.Braille)
	}
	return fmt.Sprintf("%q", s)
}

// profileName spells out what termenv detected. The constant's own name is not
// enough on its own: "Ascii" reads as a glyph problem rather than as "this
// terminal was given no colour at all".
func profileName(p termenv.Profile) string {
	switch p {
	case termenv.TrueColor:
		return "truecolor (24-bit, gradients whole)"
	case termenv.ANSI256:
		return "256 colours (gradients quantised, banding expected)"
	case termenv.ANSI:
		return "8 colours"
	case termenv.Ascii:
		return "none (monochrome)"
	default:
		return fmt.Sprintf("unrecognised (%d)", p)
	}
}

// eta formats an estimate, keeping "rclone cannot say" distinct from "no time
// left at all".
func eta(d time.Duration, known bool) string {
	if !known {
		return "unknown"
	}
	return ui.Duration(d)
}

// transferSize formats a file's total size, which rclone records as -1 when it
// could not learn it before starting. Printing that as a byte count would say
// the file is a negative size; printing it as zero would say it is empty.
func transferSize(size int64, base10 bool) string {
	if size < 0 {
		return "unknown size"
	}
	return ui.Bytes(uint64(size), base10)
}

// transferDone formats how much of a file has moved. The text log form gives
// the percentage and nothing else, so there is a byte count to print only when
// the log was written as JSON.
func transferDone(t model.Transfer, base10 bool) string {
	if !t.BytesKnown {
		return fmt.Sprintf("%d%%", t.Percentage)
	}
	return fmt.Sprintf("%d%% (%s)", t.Percentage, ui.Bytes(t.Bytes, base10))
}

// stamp formats a timestamp for the dump, distinguishing "never" from a real
// instant so an empty field is not read as the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format(time.RFC3339)
}
