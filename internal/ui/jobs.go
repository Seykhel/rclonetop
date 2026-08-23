package ui

import (
	"fmt"
	"strings"

	"github.com/Seykhel/rclonetop/internal/model"
)

// jobFor returns the job a process is writing about itself, if the log
// collector found its log file.
//
// The match is on the PID, which is exact: the log file was discovered from
// that process's own command line. A job whose process has exited keeps its
// last statistics but no longer matches anything, and so quietly stops being
// drawn.
func (m Model) jobFor(p model.Process) (model.Job, bool) {
	for _, j := range m.state.Jobs {
		if j.PID != 0 && j.PID == p.PID {
			return j, true
		}
	}
	return model.Job{}, false
}

// jobProgress renders what rclone says about its own run: how much of the work
// it set itself is done.
//
// This is the one line on the display that /proc cannot produce. The kernel's
// byte counters say what has gone past; only rclone knows how much there was to
// begin with, and therefore whether a transfer is nearly finished or has barely
// started.
func (m Model) jobProgress(p model.Process) string {
	job, ok := m.jobFor(p)
	if !ok || !job.HaveStats {
		return ""
	}
	s := job.Stats

	var parts []string
	if frac, ok := s.Done(); ok {
		parts = append(parts,
			m.gradientStyle("cpu", frac).Render(fmt.Sprintf("%.0f%%", frac*100)))
	}
	if s.TotalBytes > 0 {
		parts = append(parts,
			m.style("main_fg").Render(Bytes(s.Bytes, m.opts.Base10))+
				m.style("div_line").Render(" / ")+
				m.style("inactive_fg").Render(Bytes(s.TotalBytes, m.opts.Base10)))
	}
	switch {
	case s.TotalTransfers > 0:
		parts = append(parts,
			m.style("main_fg").Render(fmt.Sprintf("%d/%d", s.Transfers, s.TotalTransfers))+
				m.style("inactive_fg").Render(" files"))
	case s.Checks > 0:
		// A bisync with nothing to move is the healthy case, and it would look
		// idle if the only counters on the line were the transfers it did not
		// have to make.
		parts = append(parts,
			m.style("main_fg").Render(fmt.Sprint(s.Checks))+
				m.style("inactive_fg").Render(" checked"))
	}
	if s.Errors > 0 {
		errs := fmt.Sprintf("%d errors", s.Errors)
		if s.FatalError {
			errs += ", fatal"
		}
		parts = append(parts, m.style("hi_fg").Render(errs))
	}
	if s.ETAKnown {
		// Only when rclone says so. It writes "-" whenever it cannot estimate,
		// and an ETA of zero would read as "any moment now".
		parts = append(parts,
			m.style("inactive_fg").Render("ETA ")+
				m.style("main_fg").Render(Duration(s.ETA)))
	}

	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, m.style("div_line").Render(" · ")) + "\n"
}

// jobErrorsFor returns what the process's own log recorded.
//
// A job started with --log-file writes nothing to the journal, so these are
// invisible to the systemd collector: for that arrangement, which is the
// commonest one, this is the only place its errors appear at all.
func (m Model) jobErrorsFor(p model.Process) []model.LogLine {
	job, ok := m.jobFor(p)
	if !ok {
		return nil
	}
	return job.Errors
}

// sideName is how one end of a bisync pair is identified on screen: the path
// the log spelled out, or failing that the mangled name on disk.
func sideName(s model.SyncSide) string {
	if s.Path != "" {
		return s.Path
	}
	return s.Label
}
