package ui

import (
	"fmt"
	"strings"

	"github.com/Seykhel/rclonetop/internal/model"
)

// jobProgress renders what rclone says about its own run: how much of the work
// it set itself is done.
//
// This is the one line on the display that /proc cannot produce. The kernel's
// byte counters say what has gone past; only rclone knows how much there was to
// begin with, and therefore whether a transfer is nearly finished or has barely
// started.
//
// A row with no log behind it carries the zero Job, which falls through every
// branch below and renders nothing -- the same answer as before, without a
// second way of spelling "there is nothing to say".
func (m Model) jobProgress(job model.Job) string {
	if job.ReadError != "" {
		// The same distinction the throughput line makes for an unreadable
		// /proc/<pid>/io: a job that stands still because nobody can read its
		// log looks exactly like one with nothing to do, and saying which is
		// the whole point.
		return "  " + m.style("inactive_fg").Render("log unreadable: ") +
			m.style("hi_fg").Render(oneLine(job.ReadError)) + "\n"
	}
	if !job.HaveStats {
		return ""
	}
	s := job.Stats

	var parts []string
	if frac, ok := s.Done(); ok {
		parts = append(parts,
			m.magnitudeStyle("cpu", frac).Bold(true).Render(fmt.Sprintf("%.0f%%", frac*100)))
	}
	if s.TotalBytes > 0 {
		parts = append(parts,
			m.value().Render(Bytes(s.Bytes, m.opts.Base10))+
				m.style("div_line").Render(" / ")+
				m.label().Render(Bytes(s.TotalBytes, m.opts.Base10)))
	}
	switch {
	case s.TotalTransfers > 0:
		parts = append(parts,
			m.value().Render(fmt.Sprintf("%d/%d", s.Transfers, s.TotalTransfers))+
				m.label().Render(" files"))
	case s.Checks > 0:
		// A bisync with nothing to move is the healthy case, and it would look
		// idle if the only counters on the line were the transfers it did not
		// have to make.
		parts = append(parts,
			m.value().Render(fmt.Sprint(s.Checks))+
				m.label().Render(" checked"))
	}
	if s.Errors > 0 {
		errs := fmt.Sprintf("%d errors", s.Errors)
		if s.FatalError {
			errs += ", fatal"
		}
		parts = append(parts, m.style("hi_fg").Bold(true).Render(errs))
	}
	if s.ETAKnown {
		// Only when rclone says so. It writes "-" whenever it cannot estimate,
		// and an ETA of zero would read as "any moment now".
		parts = append(parts,
			m.label().Render("ETA ")+
				m.value().Render(Duration(s.ETA)))
	}

	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, m.style("div_line").Render(" · ")) + "\n"
}

// sideName is how one end of a bisync pair is identified on screen: the path
// the log spelled out, or failing that the mangled name on disk.
func sideName(s model.SyncSide) string {
	if s.Path != "" {
		return s.Path
	}
	return s.Label
}
