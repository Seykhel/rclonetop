package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
)

// staleSuccess is how long a successful run stays reassuring. Past it the
// timestamp is graded towards the warm end of the ramp: a backup that last
// succeeded three days ago is not the same as one that succeeded an hour ago,
// even though systemd calls both of them "success".
const staleSuccess = 24 * time.Hour

// denseUnits renders the systemd services and timers driving rclone.
//
// Services and their timers are folded into one line each. Presenting them
// separately would double the length of the section and split the two halves of
// a single answer: a timer's schedule is only meaningful next to the result of
// the job it starts.
func (m Model) denseUnits(width int) string {
	if len(m.state.Units) == 0 {
		return ""
	}

	timers := make(map[string]model.Unit)
	var services []model.Unit
	for _, u := range m.state.Units {
		if u.IsTimer() {
			// Two timers can start the same service. Keep the one due first,
			// since that is the answer to "when does this next run"; picking
			// arbitrarily would make the display depend on map ordering.
			if u.Triggers == "" {
				continue
			}
			if prev, ok := timers[u.Triggers]; ok && sooner(prev.NextElapse, u.NextElapse) {
				continue
			}
			timers[u.Triggers] = u
			continue
		}
		services = append(services, u)
	}
	// Timers whose service was not itself reported still deserve a line.
	for target, t := range timers {
		if !hasUnit(services, target) {
			services = append(services, model.Unit{
				Name: target, Scope: t.Scope, Source: t.Source,
			})
		}
	}
	if len(services) == 0 {
		return ""
	}

	// Failures first, then by name, so a problem never scrolls out of reach
	// behind healthy units.
	sort.SliceStable(services, func(i, j int) bool {
		fi, fj := services[i].Failed(), services[j].Failed()
		if fi != fj {
			return fi
		}
		return services[i].Name < services[j].Name
	})

	var b strings.Builder
	for _, u := range services {
		b.WriteString(m.denseUnit(u, timers[u.Name], width))
	}
	return b.String()
}

func hasUnit(units []model.Unit, name string) bool {
	for _, u := range units {
		if u.Name == name {
			return true
		}
	}
	return false
}

// denseUnit renders one service together with its timer.
func (m Model) denseUnit(u model.Unit, timer model.Unit, width int) string {
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
		suffix += m.style("inactive_fg").Render(" (system)")
	}
	suffix += "  " + m.unitState(u)

	room := width - lipgloss.Width(head) - lipgloss.Width(suffix)
	name := Truncate(strings.TrimSuffix(u.Name, ".service"), room, false)
	head += m.style("main_fg").Render(name) + suffix

	var parts []string
	if last := u.LastRun(timer.LastTrigger); !last.IsZero() {
		if u.Running() {
			// A job that is up has not "last run" at any point: the timestamp
			// measures how long it has been going, and "last 14h ago" reads as
			// though it had finished.
			parts = append(parts, m.style("inactive_fg").Render("running for ")+
				m.style("main_fg").Render(Duration(m.now.Sub(last))))
		} else {
			parts = append(parts, m.style("inactive_fg").Render("last ")+
				m.runStyle(u, last).Render(Ago(m.now.Sub(last))))
		}
	}
	switch {
	case !timer.NextElapse.IsZero():
		parts = append(parts, m.style("inactive_fg").Render("next ")+
			m.style("main_fg").Render(m.until(timer.NextElapse)))
	case timer.Name != "":
		// A timer with no next elapse has been stopped. On this setup that is
		// exactly what a failed bisync does to its own schedule, so it must not
		// read as a blank.
		parts = append(parts, m.style("hi_fg").Render("timer stopped"))
	}
	if exit := u.Exit(); exit != "" {
		parts = append(parts, m.style("hi_fg").Render(exit))
	}

	line := head
	if len(parts) > 0 {
		line += "\n  " + strings.Join(parts, m.style("div_line").Render(" · "))
	}
	line += "\n"

	// The most recent journal error, if any. One line: the point is to say
	// that something went wrong and roughly when, not to be a log viewer.
	if len(u.Errors) > 0 {
		e := u.Errors[len(u.Errors)-1]
		prefix := "  " + m.style("hi_fg").Render("! ") +
			m.style("inactive_fg").Render(Ago(m.now.Sub(e.At))+"  ")
		if room := width - lipgloss.Width(prefix); room > 0 {
			line += prefix + m.style("hi_fg").Render(Truncate(oneLine(e.Message), room, false)) + "\n"
		}
		if n := len(u.Errors); n > 1 {
			line += "  " + m.style("inactive_fg").Render(
				fmt.Sprintf("  and %d more recent", n-1)) + "\n"
		}
	}

	return line
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
		return m.gradientStyle("temp", 1).Bold(true).Render(label)
	case u.Running():
		// Covers the oneshot case too: systemd holds a oneshot at "activating"
		// for the whole of its ExecStart, so a backup in flight is never
		// "active" and would otherwise never be called running.
		return m.gradientStyle("free", 1).Render("running")
	case u.ActiveState == "active":
		// active/exited: a oneshot with RemainAfterExit=yes. systemd counts it
		// as active even though nothing is executing.
		return m.gradientStyle("free", 0.4).Render("active")
	case u.ActiveState == "deactivating":
		return m.gradientStyle("cpu", 0.5).Render("stopping")
	case u.ActiveState == "reloading":
		return m.gradientStyle("cpu", 0.5).Render("reloading")
	case u.ActiveState == "":
		return m.style("inactive_fg").Render("scheduled")
	default:
		return m.style("inactive_fg").Render("idle")
	}
}

// sooner reports whether a comes before b, treating "never" as last.
func sooner(a, b time.Time) bool {
	switch {
	case a.IsZero():
		return false
	case b.IsZero():
		return true
	default:
		return a.Before(b)
	}
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
		return m.gradientStyle("temp", 0)
	}
	return m.gradientStyle("temp", float64(age)/float64(staleSuccess))
}

// oneLine flattens a journal message. Entries routinely span several lines, and
// the dense view has room for one.
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}
