package ui

import (
	"fmt"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/box"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"sort"
	"strings"
	"time"
)

type detailEntry struct{ key, text string }
type detailLine struct {
	key, text string
	part      int
}

func instant(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Format(time.RFC3339)
}

// Detail reads the resolved subject only. Its logical entries keep the reading
// position stable when earlier sections grow, or text wraps at a new width.
func (m Model) detailEntries() []detailEntry {
	entries := []detailEntry{{"title", "Subject details"}}
	add := func(key, text string) { entries = append(entries, detailEntry{key, text}) }
	section := func(key, title string) { add(key, title) }
	v := m.state.Resolve()
	s, ok := findSubject(v, m.detailSubject)
	if !ok {
		add("gone", "Subject no longer available")
		return entries
	}
	var p *model.Process
	var u, timer model.Unit
	var job model.Job
	var errs []model.LogLine
	var rc *model.RCStats
	if s.process != nil {
		r := s.process
		p = &r.Process
		u, timer, job, errs, rc = r.Unit, r.Timer, r.Job, r.Errors, r.RCStats
	} else {
		r := s.unit
		u, timer, job, errs = r.Unit, r.Timer, r.Job, r.Errors
	}
	section("identity", "Identity")
	add("subject", s.label())
	if p != nil {
		add("process", fmt.Sprintf("%s pid %d [%s]", p.Kind, p.PID, p.Source))
		for i, path := range p.Paths {
			add(fmt.Sprintf("path/%d", i), "Operand: "+path)
		}
		if len(p.Paths) == 0 {
			for i, path := range p.Remotes {
				add(fmt.Sprintf("path/%d", i), "Remote: "+path)
			}
		}
		add("started", "Process started: "+instant(p.StartedAt))
	}
	if u.Name != "" {
		add("unit", "Service: "+u.Name+" ("+u.Scope+") ["+string(u.Source)+"]")
	}
	if job.LogFile != "" {
		add("log", "Log file: "+job.LogFile)
	}
	section("state", "State and outcome")
	if p != nil {
		add("running", "Process running")
	}
	if u.Name != "" {
		add("unit-state", "Service: "+stripStyles(m.unitState(u))+"; active="+unknown(u.ActiveState)+"; sub="+unknown(u.SubState)+"; result="+unknown(u.Result))
		if exit := u.Exit(); exit != "" {
			add("exit", exit)
		}
	}
	add("outcome", "Log outcome: "+unknown(job.Outcome))
	section("times", "Times and scheduling")
	if u.Name != "" {
		add("unit-start", "Service left inactive: "+instant(u.InactiveExit))
		add("unit-active", "Service entered active: "+instant(u.ActiveEnter))
		add("unit-end", "Service entered inactive: "+instant(u.InactiveEnter))
	}
	if timer.Name != "" {
		add("timer", "Timer: "+timer.Name+" ("+timer.Scope+")")
		add("timer-last", "Timer last triggered: "+instant(timer.LastTrigger))
		next := instant(timer.NextElapse)
		if timer.NextElapse.IsZero() {
			next = "stopped"
		}
		add("timer-next", "Timer next: "+next)
	} else {
		add("timer", "Timer unavailable")
	}
	add("log-at", "Last parsed log entry: "+instant(job.At)+" (not a completion time)")
	section("statistics", "Statistics")
	if p != nil {
		add("memory", fmt.Sprintf("Process RSS: %s; threads: %d [%s]", Bytes(p.RSS, m.opts.Base10), p.Threads, p.Source))
		if !p.IOAvailable {
			add("io", "Process throughput unavailable")
		} else {
			add("io", fmt.Sprintf("Process read: %s; write: %s [%s]", Rate(p.ReadRate, m.opts.Base10), Rate(p.WriteRate, m.opts.Base10), p.Source))
			add("io-total", fmt.Sprintf("Process counters read: %s; write: %s [%s]", Bytes(p.ReadTotal, m.opts.Base10), Bytes(p.WriteTotal, m.opts.Base10), p.Source))
		}
	}
	if !job.HaveStats {
		add("stats-none", "Statistics unavailable")
	} else {
		st := job.Stats
		fields := []struct {
			field            model.StatsFields
			key, name, value string
		}{
			{model.StatsBytes, "bytes", "Transferred bytes", Bytes(st.Bytes, m.opts.Base10)},
			{model.StatsTotalBytes, "total", "Total bytes", Bytes(st.TotalBytes, m.opts.Base10)},
			{model.StatsTransfers, "transfers", "Transfers", fmt.Sprint(st.Transfers)},
			{model.StatsTotalTransfers, "transfers-total", "Total transfers", fmt.Sprint(st.TotalTransfers)},
			{model.StatsChecks, "checks", "Checks", fmt.Sprint(st.Checks)},
			{model.StatsTotalChecks, "checks-total", "Total checks", fmt.Sprint(st.TotalChecks)},
			{model.StatsErrors, "errors-count", "Error count", fmt.Sprint(st.Errors)},
			{model.StatsFatalError, "fatal", "Fatal error", fmt.Sprint(st.FatalError)},
			{model.StatsDeletes, "deletes", "Deletes", fmt.Sprint(st.Deletes)},
			{model.StatsRenames, "renames", "Renames", fmt.Sprint(st.Renames)},
			{model.StatsSpeed, "speed", "Average speed", Rate(st.Speed, m.opts.Base10)},
			{model.StatsElapsed, "elapsed", "Elapsed", Duration(st.Elapsed)},
			{model.StatsETA, "eta", "ETA", Duration(st.ETA)},
		}
		for _, f := range fields {
			value := f.value
			known := st.Known == 0 || st.Known&f.field != 0
			if f.field == model.StatsETA && !st.ETAKnown {
				known = false
			}
			if !known {
				value = "unknown"
			}
			source := st.Source
			if src := st.Sources[f.field]; src != "" {
				source = src
			}
			suffix := ""
			if known && source != "" {
				suffix = " [" + string(source) + "]"
			}
			add("stats/"+f.key, f.name+": "+value+suffix)
		}
	}
	section("files", "Files in flight")
	switch {
	case job.Transferring == nil:
		add("files-none", "File list unavailable")
	case len(job.Transferring) == 0:
		add("files-none", "No files in flight")
	default:
		for i, f := range job.Transferring {
			add(fmt.Sprintf("file/%d", i), f.Name)
		}
	}
	section("errors", "Retained errors")
	if len(errs) == 0 {
		add("errors-none", "No retained errors (not a complete log)")
	}
	for i, e := range errs {
		source := model.SourceLog
		if i < len(u.Errors) {
			source = model.SourceSystemd
		}
		add(fmt.Sprintf("error/%s/%s/%d", source, instant(e.At), i), fmt.Sprintf("[%s] %s %s", source, instant(e.At), e.Message))
	}
	if rc != nil {
		add("rc-at", "RC last observation: "+instant(rc.At))
	}
	section("sources", "Sources")
	if job.ReadError != "" {
		add("log-error", "Log unreadable: "+job.ReadError)
	}
	sources := map[model.Source]bool{}
	for source := range v.Seen {
		sources[source] = true
	}
	for source := range v.Errors {
		sources[source] = true
	}
	names := make([]string, 0, len(sources))
	for source := range sources {
		names = append(names, string(source))
	}
	sort.Strings(names)
	for _, name := range names {
		src := model.Source(name)
		line := name + ": last collected " + instant(v.Seen[src])
		if err := v.Errors[src]; err != nil {
			line += "; source failed, retained data may be stale: " + err.Error()
		}
		add("source/"+name, line)
	}
	return entries
}
func unknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
func (m Model) detailLines(width int) []detailLine {
	var lines []detailLine
	for _, entry := range m.detailEntries() {
		for i, line := range strings.Split(ansi.Wrap(entry.text, max(width, 1), ""), "\n") {
			lines = append(lines, detailLine{entry.key, m.value().Render(line), i})
		}
	}
	return lines
}

const detailHint = "Esc back  |  ? help  |  Up/Down PgUp/PgDn scroll  |  q quit"

func (m Model) detailGeometry() (width, rows int, framed bool) {
	w, h := effectiveWidth(m.width), effectiveHeight(m.height)
	inner := min(88, w-2)
	if inner >= len("Subject details") && len(m.detailLines(inner))+3 <= h {
		return inner, len(m.detailLines(inner)), true
	}
	return w, max(h-1, 0), false
}
func (m *Model) restoreDetailAnchor() {
	width, rows, framed := m.detailGeometry()
	lines := m.detailLines(width)
	if framed {
		m.detailOffset = 0
		return
	}
	for i, line := range lines {
		if line.key == m.detailAnchor && line.part <= m.detailPart {
			m.detailOffset = i
		}
	}
	m.detailOffset = min(max(m.detailOffset, 0), max(len(lines)-rows, 0))
}
func (m *Model) scrollDetail(action keyAction) {
	width, rows, framed := m.detailGeometry()
	if framed {
		return
	}
	switch action {
	case actionScrollUp:
		m.detailOffset--
	case actionScrollDown:
		m.detailOffset++
	case actionPageUp:
		m.detailOffset -= max(rows, 1)
	case actionPageDown:
		m.detailOffset += max(rows, 1)
	default:
		return
	}
	lines := m.detailLines(width)
	m.detailOffset = min(max(m.detailOffset, 0), max(len(lines)-rows, 0))
	line := lines[min(m.detailOffset, len(lines)-1)]
	m.detailAnchor, m.detailPart = line.key, line.part
}
func (m Model) detailFooter(width int) string {
	for _, s := range []string{detailHint, "Esc back  ? help  q quit", "Esc back", "Esc"} {
		if lipgloss.Width(s) <= width {
			return m.label().Render(s)
		}
	}
	return "q"
}
func (m Model) renderDetail() string {
	width, rows, framed := m.detailGeometry()
	lines := m.detailLines(width)
	if !framed {
		out := make([]string, 0, rows+1)
		for i := 0; i < rows; i++ {
			line := ""
			if at := m.detailOffset + i; at < len(lines) {
				line = lines[at].text
			}
			out = append(out, fitCell(line, width))
		}
		out = append(out, fitCell(m.detailFooter(width), width))
		return strings.Join(out, "\n")
	}
	body := make([]string, 0, len(lines))
	for _, line := range lines[1:] {
		body = append(body, line.text)
	}
	body = append(body, m.detailFooter(width))
	frame := box.Box{Width: width + 2, Height: len(body) + 2, Runes: m.boxRunes()}
	panel := m.frameRows(frame, "Subject details", box.NoHotkey, m.style("div_line"), body)
	return m.overlay(panel, frame.Width, m.monitorView())
}

// Both modal views compose onto a visible rectangle. Help can use detail as its
// background without reaching back through View and recursively opening itself.
func (m Model) overlay(panel []string, panelWidth int, background string) string {
	width, height := effectiveWidth(m.width), effectiveHeight(m.height)
	behind := strings.Split(background, "\n")
	out := make([]string, height)
	x, y := (width-panelWidth)/2, (height-len(panel))/2
	for i := range out {
		line := ""
		if i < len(behind) {
			line = behind[i]
		}
		out[i] = fitCell(line, width)
		if i >= y && i < y+len(panel) {
			out[i] = ansi.Cut(out[i], 0, x) + lipgloss.NewStyle().Background(m.opts.Theme.Color("main_bg").Lipgloss()).Render(panel[i-y]) + ansi.Cut(out[i], x+panelWidth, width)
		}
	}
	return strings.Join(out, "\n")
}
