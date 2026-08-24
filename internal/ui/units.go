package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/theme"
)

// staleSuccess is how long a successful run stays reassuring. Past it the
// timestamp is graded towards the warm end of the ramp: a backup that last
// succeeded three days ago is not the same as one that succeeded an hour ago,
// even though systemd calls both of them "success".
const staleSuccess = 24 * time.Hour

// errorFadeWindow is how long a journal error takes to cool from the alarm
// colour to the inactive one. Six hours: long enough that a failure overnight
// is still visibly a failure in the morning, short enough that it stops
// competing with whatever is wrong now.
const errorFadeWindow = 6 * time.Hour

// denseUnits renders the systemd services and timers driving rclone.
//
// Which units get a line, in what order, and with which timer folded into them
// is settled by State.Resolve; what is left here is how one of those rows is
// written out.
func (m Model) denseUnits(rows []model.UnitRow, width int) string {
	var b strings.Builder
	for _, row := range rows {
		b.WriteString(m.denseUnit(row, width))
	}
	return b.String()
}

// renderErrors draws the most recent journal error and a count of the rest.
// One line: the point is to say that something went wrong and roughly when,
// not to be a log viewer.
//
// The alarm colour cools towards the inactive one as the entry ages. A failure
// from five hours ago is worth knowing about and worth keeping on screen, but
// painting it as brightly as one from a minute ago says something untrue about
// how urgent it is.
func (m Model) renderErrors(errs []model.LogLine, width int) string {
	if len(errs) == 0 {
		return ""
	}
	e := errs[len(errs)-1]
	faded := m.fadedAlarm(e.At)

	prefix := "  " + faded.Render("! ") +
		m.style("inactive_fg").Render(Ago(m.now.Sub(e.At))+"  ")
	room := width - lipgloss.Width(prefix)
	if room <= 0 {
		return ""
	}

	out := prefix + faded.Render(Truncate(oneLine(e.Message), room, false)) + "\n"
	if n := len(errs); n > 1 {
		out += "  " + m.style("inactive_fg").Render(
			fmt.Sprintf("  and %d more recent", n-1)) + "\n"
	}
	return out
}

// denseUnit renders one service together with its timer.
func (m Model) denseUnit(row model.UnitRow, width int) string {
	u, timer := row.Unit, row.Timer

	head := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color("proc_box").Lipgloss()).
		Bold(true).
		Render(fmt.Sprintf("%-7s", "UNIT"))

	// The suffix is built first so the name is budgeted against what is
	// actually left, rather than against a guessed reserve. The state label
	// varies from "idle" to "exec-condition", and the scope tag adds nine more.
	var suffix string
	if u.Scope == "system" {
		// Worth saying: a system unit is not the one a user's own timer runs.
		suffix += m.label().Render(" (system)")
	}
	suffix += "  " + m.unitState(u)

	room := width - lipgloss.Width(head) - lipgloss.Width(suffix)
	name := Truncate(strings.TrimSuffix(u.Name, ".service"), room, false)
	head += m.value().Render(name) + suffix

	var parts []string
	if last := row.LastRun; !last.IsZero() {
		if u.Running() {
			// A job that is up has not "last run" at any point: the timestamp
			// measures how long it has been going, and "last 14h ago" reads as
			// though it had finished.
			parts = append(parts, m.label().Render("running for ")+
				m.value().Render(Duration(m.now.Sub(last))))
		} else {
			parts = append(parts, m.label().Render("last ")+
				m.runStyle(u, last).Render(Ago(m.now.Sub(last))))
		}
	}
	switch {
	case !timer.NextElapse.IsZero():
		parts = append(parts, m.label().Render("next ")+
			m.value().Render(m.until(timer.NextElapse)))
	case timer.Name != "":
		// A timer with no next elapse has been stopped. On this setup that is
		// exactly what a failed bisync does to its own schedule, so it must not
		// read as a blank.
		parts = append(parts, m.alarm().Render("timer stopped"))
	}
	if exit := u.Exit(); exit != "" {
		parts = append(parts, m.alarm().Render(exit))
	}

	// What the unit's own log said, which systemd never sees: a job started
	// with --log-file writes nothing to the journal, so between runs this is
	// the only account of how the last one went.
	if row.Job.Outcome != "" {
		parts = append(parts, m.outcomeStyle(row.Job).Render(row.Job.Outcome))
	}

	line := head
	if len(parts) > 0 {
		line += "\n  " + strings.Join(parts, m.style("div_line").Render(" · "))
	}
	line += "\n"
	line += m.renderErrors(row.Errors, width)

	return line
}

// outcomeStyle colours how a run ended. Only "successful" is good news; the
// rest -- aborted, interrupted -- are the reason anyone is looking.
func (m Model) outcomeStyle(job model.Job) lipgloss.Style {
	if job.Outcome == "successful" {
		return m.accentStyle(accentRunning)
	}
	return m.style("hi_fg")
}

// unitState renders the state badge.
//
// systemd leaves a finished oneshot "inactive" whether it worked or not, so the
// active state alone cannot answer "did it work". Result can, and that is what
// decides the colour here.
func (m Model) unitState(u model.Unit) string {
	switch {
	case u.Failed():
		label := u.Result
		if u.ActiveState == "failed" {
			label = "failed"
		}
		return m.accentStyle(accentFailed).Render(label)
	case u.Running():
		// Covers the oneshot case too: systemd holds a oneshot at "activating"
		// for the whole of its ExecStart, so a backup in flight is never
		// "active" and would otherwise never be called running.
		return m.accentStyle(accentRunning).Render("running")
	case u.ActiveState == "active":
		// active/exited: a oneshot with RemainAfterExit=yes. systemd counts it
		// as active even though nothing is executing.
		return m.accentStyle(accentActive).Render("active")
	case u.ActiveState == "deactivating":
		return m.accentStyle(accentBusy).Render("stopping")
	case u.ActiveState == "reloading":
		return m.accentStyle(accentBusy).Render("reloading")
	case u.ActiveState == "":
		return m.style("inactive_fg").Render("scheduled")
	default:
		return m.style("inactive_fg").Render("idle")
	}
}

// fadedAlarm grades the alarm colour by how long ago something happened.
func (m Model) fadedAlarm(at time.Time) lipgloss.Style {
	frac := 0.0
	if !at.IsZero() {
		frac = m.now.Sub(at).Seconds() / errorFadeWindow.Seconds()
	}
	c := theme.Blend(
		m.opts.Theme.Color("hi_fg"),
		m.opts.Theme.Color("inactive_fg"),
		frac)
	return lipgloss.NewStyle().Foreground(c.Lipgloss())
}

// until renders how long remains before an instant.
//
// A due-now timer is routine: list-timers is a five-second-old snapshot, and
// the elapse stays in the past for as long as the job it started runs. Duration
// answers "-" for a non-positive interval, and in this codebase that dash means
// "unknown" -- which this is not.
func (m Model) until(t time.Time) string {
	d := t.Sub(m.now)
	if d <= 0 {
		return "due now"
	}
	return "in " + Duration(d)
}

// runStyle grades how long ago a run was, so a stale success stops looking like
// a fresh one.
func (m Model) runStyle(u model.Unit, last time.Time) lipgloss.Style {
	if u.Failed() {
		return m.style("hi_fg")
	}
	age := m.now.Sub(last)
	if age <= 0 {
		return m.magnitudeStyle("temp", 0)
	}
	return m.magnitudeStyle("temp", float64(age)/float64(staleSuccess))
}

// oneLine flattens a journal message. Entries routinely span several lines, and
// the dense view has room for one.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}
