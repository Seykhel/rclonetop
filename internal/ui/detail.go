package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/box"
)

type detailEntry struct {
	key, text string
	heading   bool
	labels    []detailLabel // byte ranges of explicitly supplied field prefixes
}
type detailLabel struct{ start, end int }
type detailLine struct {
	key, text string
	start     int // byte offset in the logical entry before wrapping
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
	entries := []detailEntry{{key: "title", text: "Subject details", heading: true}}
	add := func(key, text string) { entries = append(entries, detailEntry{key: key, text: text}) }
	field := func(key, name, value string) {
		prefix := name + ": "
		entries = append(entries, detailEntry{key: key, text: prefix + value, labels: []detailLabel{{0, len(prefix)}}})
	}
	// Parts alternate explicit label prefixes and their values.
	compound := func(key string, parts ...string) {
		entry := detailEntry{key: key}
		for i := 0; i < len(parts); i += 2 {
			start := len(entry.text)
			entry.text += parts[i]
			entry.labels = append(entry.labels, detailLabel{start, len(entry.text)})
			entry.text += parts[i+1]
		}
		entries = append(entries, entry)
	}
	section := func(key, title string) { entries = append(entries, detailEntry{key: key, text: title, heading: true}) }
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
			field(fmt.Sprintf("path/%d", i), "Operand", path)
		}
		if len(p.Paths) == 0 {
			for i, path := range p.Remotes {
				field(fmt.Sprintf("path/%d", i), "Remote", path)
			}
		}
		field("started", "Process started", instant(p.StartedAt))
	}
	if u.Name != "" {
		field("unit", "Service", u.Name+" ("+u.Scope+") ["+string(u.Source)+"]")
	}
	if job.LogFile != "" {
		field("log", "Log file", job.LogFile)
	}
	section("state", "State and outcome")
	if p != nil {
		add("running", "Process running")
	}
	if u.Name != "" {
		compound("unit-state", "Service: ", stripStyles(m.unitState(u)), "; active=", unknown(u.ActiveState), "; sub=", unknown(u.SubState), "; result=", unknown(u.Result))
		if exit := u.Exit(); exit != "" {
			add("exit", exit)
		}
	}
	field("outcome", "Log outcome", unknown(job.Outcome))
	section("times", "Times and scheduling")
	if u.Name != "" {
		field("unit-start", "Service left inactive", instant(u.InactiveExit))
		field("unit-active", "Service entered active", instant(u.ActiveEnter))
		field("unit-end", "Service entered inactive", instant(u.InactiveEnter))
	}
	if timer.Name != "" {
		field("timer", "Timer", timer.Name+" ("+timer.Scope+")")
		field("timer-last", "Timer last triggered", instant(timer.LastTrigger))
		next := instant(timer.NextElapse)
		if timer.NextElapse.IsZero() {
			next = "stopped"
		}
		field("timer-next", "Timer next", next)
	} else {
		add("timer", "Timer unavailable")
	}
	field("log-at", "Last parsed log entry", instant(job.At)+" (not a completion time)")
	section("statistics", "Statistics")
	if p != nil {
		compound("memory", "Process RSS: ", Bytes(p.RSS, m.opts.Base10), "; threads: ", fmt.Sprintf("%d [%s]", p.Threads, p.Source))
		if !p.IOAvailable {
			add("io", "Process throughput unavailable")
		} else {
			compound("io", "Process read: ", Rate(p.ReadRate, m.opts.Base10), "; write: ", Rate(p.WriteRate, m.opts.Base10)+" ["+string(p.Source)+"]")
			compound("io-total", "Process counters read: ", Bytes(p.ReadTotal, m.opts.Base10), "; write: ", Bytes(p.WriteTotal, m.opts.Base10)+" ["+string(p.Source)+"]")
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
			field("stats/"+f.key, f.name, value+suffix)
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
			pct, size, speed, moved, eta := "unknown", "unknown", "unknown", "unknown", "unknown"
			if f.Percentage >= 0 {
				pct = fmt.Sprintf("%d%%", f.Percentage)
			}
			if f.Size >= 0 {
				size = Bytes(uint64(f.Size), m.opts.Base10)
			}
			if f.Speed >= 0 {
				speed = Rate(f.Speed, m.opts.Base10)
			}
			if f.BytesKnown {
				moved = Bytes(f.Bytes, m.opts.Base10)
			}
			if f.ETAKnown {
				eta = Duration(f.ETA)
			}
			compound(fmt.Sprintf("file/%d/measurements", i), "Progress: ", pct, "; moved: ", moved, "; size: ", size, "; speed: ", speed, "; ETA: ", eta)
		}
	}
	section("errors", "Retained errors")
	if len(errs) == 0 {
		add("errors-none", "No retained errors (not a complete log)")
	}
	errorOccurrences := map[string]int{}
	for i, e := range errs {
		source := model.SourceLog
		if i < len(u.Errors) {
			source = model.SourceSystemd
		}
		identity := fmt.Sprintf("error/%s/%s/%s", source, e.At.Format(time.RFC3339Nano), e.Message)
		occurrence := errorOccurrences[identity]
		errorOccurrences[identity]++
		add(fmt.Sprintf("%s/%d", identity, occurrence), fmt.Sprintf("[%s] %s %s", source, instant(e.At), e.Message))
	}
	if rc != nil {
		section("rc", "RC daemon and jobs")
		field("rc-address", "RC endpoint", rc.Addr)
		field("rc-at", "RC last observation", instant(rc.At))
		d := rc.Daemon
		field("rc-version", "rclone version", unknown(d.Version))
		heap, sys := "unknown", "unknown"
		if d.Memory.HeapAllocSet {
			heap = Bytes(d.Memory.HeapAlloc, m.opts.Base10)
		}
		if d.Memory.SysSet {
			sys = Bytes(d.Memory.Sys, m.opts.Base10)
		}
		field("rc-heap", "Heap allocated", heap+" [rc]")
		field("rc-sys", "System memory", sys+" [rc]")
		bandwidth := "unknown"
		if d.Bandwidth.Known {
			if d.Bandwidth.BytesPerSecond < 0 {
				bandwidth = "unlimited"
			} else {
				bandwidth = Rate(float64(d.Bandwidth.BytesPerSecond), m.opts.Base10)
			}
		}
		field("rc-bandwidth", "Bandwidth limit", bandwidth)
		switch {
		case !d.VFS.Answered:
			field("rc-vfs", "VFS cache", "unknown")
		case !d.VFS.DiskCache:
			field("rc-vfs", "VFS disk cache", "disabled")
		default:
			bytes, files := "unknown", "unknown"
			if d.VFS.BytesSet {
				bytes = Bytes(d.VFS.BytesUsed, m.opts.Base10)
			}
			if d.VFS.FilesSet {
				files = fmt.Sprint(d.VFS.Files)
			}
			compound("rc-vfs", "VFS disk cache: ", bytes, "; files: ", files)
		}
		for _, j := range rc.Jobs {
			key := fmt.Sprintf("rc-job/%d", j.ID)
			outcome := "running"
			if j.Finished {
				outcome = "finished; outcome unknown"
				if j.SuccessKnown {
					if j.Success {
						outcome = "successful"
					} else {
						outcome = "failed"
					}
				}
			}
			compound(key, fmt.Sprintf("RC job %d: ", j.ID), outcome, "; group: ", unknown(j.Group))
			compound(key+"/times", "Started: ", instant(j.StartTime), "; ended: ", instant(j.EndTime), "; duration: ", Duration(j.Duration))
			if j.Error != "" {
				add(key+"/error", "[rc] "+j.Error)
			}
		}
	}
	section("sources", "Sources")
	if job.ReadError != "" {
		field("log-error", "Log unreadable", job.ReadError)
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
		line := "last collected " + instant(v.Seen[src])
		if err := v.Errors[src]; err != nil {
			line += "; source failed, retained data may be stale: " + err.Error()
		}
		field("source/"+name, name, line)
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
		offset := 0
		for _, line := range strings.Split(ansi.Wrap(entry.text, max(width, 1), ""), "\n") {
			// Wrap may consume a separator. Locate each plain-text fragment in order
			// before styling so field metadata survives both wrapping and scrolling.
			start := offset
			if found := strings.Index(entry.text[offset:], line); found >= 0 {
				start += found
			}
			end := min(start+len(line), len(entry.text))
			styled := m.value().Render(line)
			if entry.heading {
				styled = m.title().Render(line)
			} else if len(entry.labels) > 0 {
				var out strings.Builder
				cursor := 0
				for _, label := range entry.labels {
					lo, hi := max(label.start, start)-start, min(label.end, end)-start
					if lo >= hi {
						continue
					}
					out.WriteString(m.value().Render(line[cursor:lo]))
					out.WriteString(m.label().Render(line[lo:hi]))
					cursor = hi
				}
				out.WriteString(m.value().Render(line[cursor:]))
				styled = out.String()
			}
			lines = append(lines, detailLine{entry.key, styled, start})
			offset = end
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
		if line.key == m.detailAnchor && line.start <= m.detailByte {
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
	m.detailAnchor, m.detailByte = line.key, line.start
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
