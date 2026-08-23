package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/series"
)

// minRateScale is the floor for the gradient's upper bound, so that a few
// kilobytes per second on an otherwise idle host do not light up as if the link
// were saturated.
const minRateScale = 1 << 20 // 1 MiB/s

// boxColorFor maps an rclone subcommand onto one of the theme's four box
// colours. Reusing btop's palette slots keeps rclonetop visually consistent
// with it, and gives each kind of workload a stable identifying colour.
func boxColorFor(k model.Kind) string {
	switch k {
	case model.KindMount, model.KindServe, model.KindRCD:
		return "net_box" // long-lived services
	case model.KindSync, model.KindBisync:
		return "mem_box" // scheduled, data-moving jobs
	case model.KindCopy, model.KindMove:
		return "cpu_box"
	default:
		return "proc_box"
	}
}

// renderDense draws preset 0: the whole picture in as few lines as possible.
func (m Model) renderDense() string {
	width := effectiveWidth(m.width)

	var b strings.Builder
	b.WriteString(m.denseHeader(width))
	b.WriteString("\n\n")

	procs := append([]model.Process(nil), m.state.Processes...)
	switch {
	case len(m.state.Seen) == 0:
		// Before any collector has reported, "nothing is running" would be a
		// claim rclonetop has not yet checked.
		b.WriteString(m.style("inactive_fg").Render("collecting…"))
		b.WriteString("\n")
	case len(procs) == 0:
		b.WriteString(m.style("inactive_fg").Render("no rclone process running"))
		b.WriteString("\n")
	default:
		// Long-lived services first, then the busiest: a mount that is always
		// there should not jump around as one-shot jobs come and go.
		sort.SliceStable(procs, func(i, j int) bool {
			li, lj := isService(procs[i].Kind), isService(procs[j].Kind)
			if li != lj {
				return li
			}
			return procs[i].ReadRate+procs[i].WriteRate > procs[j].ReadRate+procs[j].WriteRate
		})
		for i, p := range procs {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.denseProcess(p, width))
		}
	}

	for _, mnt := range m.orphanMounts() {
		b.WriteString("\n")
		b.WriteString(m.denseOrphanMount(mnt, width))
	}
	for _, pair := range m.state.SyncPairs {
		b.WriteString("\n")
		b.WriteString(m.denseSyncPair(pair, width))
	}
	if line := m.denseUnits(width); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
	}
	if line := m.denseCaches(); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.denseFooter(width))

	// A final clamp, applied once for every section rather than trusted to
	// each one's own arithmetic. Some content genuinely cannot fit a very
	// narrow terminal -- the fixed labels alone outgrow thirty columns -- and
	// a line that wraps corrupts the layout of everything below it. lipgloss
	// cuts without breaking the escape sequences.
	return lipgloss.NewStyle().MaxWidth(width).Render(b.String())
}

// orphanMounts are FUSE mounts with no live rclone process behind them. That
// combination is a real failure mode -- the process died and left the
// mountpoint wedged -- and it is invisible to a process listing alone.
func (m Model) orphanMounts() []model.Mount {
	if len(m.state.Mounts) == 0 {
		return nil
	}
	served := make(map[string]bool, len(m.state.Processes))
	for _, p := range m.state.Processes {
		if p.Kind == model.KindMount {
			served[p.Target] = true
		}
	}

	var orphans []model.Mount
	for _, mnt := range m.state.Mounts {
		if !served[mnt.Mountpoint] {
			orphans = append(orphans, mnt)
		}
	}
	return orphans
}

// denseOrphanMount renders a mount whose process is gone.
func (m Model) denseOrphanMount(mnt model.Mount, width int) string {
	head := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color("net_box").Lipgloss()).
		Bold(true).
		Render(fmt.Sprintf("%-7s", "MOUNT"))

	head += m.style("proc_misc").Render(mnt.Remote) +
		m.style("div_line").Render(" → ") +
		m.style("main_fg").Render(Truncate(shortenHome(mnt.Mountpoint), width-lipgloss.Width(head)-16, true))

	warn := "  " + m.style("hi_fg").Render("mounted, but no rclone process is serving it")
	return head + "\n" + warn + "\n"
}

// denseSyncPair renders a bisync session from its cached listings.
func (m Model) denseSyncPair(p model.SyncPair, width int) string {
	head := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color("mem_box").Lipgloss()).
		Bold(true).
		Render(fmt.Sprintf("%-7s", "SYNC"))

	// The paths the log recovered when it has seen this pair, and the mangled
	// filename when it has not.
	left, right := sideName(p.Left), sideName(p.Right)
	room := (width - lipgloss.Width(head) - 4) / 2
	head += m.style("main_fg").Render(Truncate(left, room, true)) +
		m.style("div_line").Render(" ⇄ ") +
		m.style("main_fg").Render(Truncate(right, room, true))

	// The two censuses, side by side. Equal counts and equal totals are the
	// quickest visual confirmation that a pair is healthy.
	sizes := "  " +
		m.side(p.Left) +
		m.style("div_line").Render("  ⇄  ") +
		m.side(p.Right)

	var status []string
	if p.Drift == 0 && p.Left.Files > 0 {
		status = append(status, m.gradientStyle("free", 1).Render("in sync"))
	} else if p.Drift > 0 {
		status = append(status, m.style("hi_fg").Render(fmt.Sprintf("%d differing", p.Drift)))
	}
	if !p.ListedAt.IsZero() {
		status = append(status, m.style("inactive_fg").Render("listed ")+
			m.style("main_fg").Render(Ago(m.now.Sub(p.ListedAt))))
	}
	if !p.FailedAt.IsZero() {
		status = append(status, m.style("hi_fg").Render("last failure "+Ago(m.now.Sub(p.FailedAt))))
	}

	line := head + "\n" + sizes + "\n"
	if len(status) > 0 {
		line += "  " + strings.Join(status, m.style("div_line").Render(" · ")) + "\n"
	}
	return line
}

// side renders one end's census.
func (m Model) side(s model.SyncSide) string {
	return m.style("main_fg").Render(fmt.Sprint(s.Files)) +
		m.style("inactive_fg").Render(" files ") +
		m.gradientStyle("used", 0.35).Render(Bytes(s.Bytes, m.opts.Base10))
}

// denseCaches renders rclone's local cache footprint on a single line.
func (m Model) denseCaches() string {
	if len(m.state.Caches) == 0 {
		return ""
	}

	head := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color("cpu_box").Lipgloss()).
		Bold(true).
		Render(fmt.Sprintf("%-7s", "CACHE"))

	var parts []string
	var scannedAt time.Time
	for _, c := range m.state.Caches {
		parts = append(parts,
			m.style("inactive_fg").Render(c.Kind+" ")+
				m.gradientStyle("cached", 0.6).Render(Bytes(c.Bytes, m.opts.Base10))+
				m.style("inactive_fg").Render(fmt.Sprintf(" (%d files)", c.Files)))
		if c.ScannedAt.After(scannedAt) {
			scannedAt = c.ScannedAt
		}
	}

	line := head + strings.Join(parts, m.style("div_line").Render(" · "))
	if !scannedAt.IsZero() {
		// The walk is expensive and therefore infrequent, so the figures are
		// always somewhat stale. Saying how stale is the honest way to show
		// them.
		line += m.style("div_line").Render(" · ") +
			m.style("inactive_fg").Render("scanned "+Ago(m.now.Sub(scannedAt)))
	}
	return line
}

func isService(k model.Kind) bool {
	return k == model.KindMount || k == model.KindServe || k == model.KindRCD
}

// denseHeader is the title line: name, host and clock, separated by a rule that
// fills the terminal width.
func (m Model) denseHeader(width int) string {
	left := m.style("title").Bold(true).Render("rclonetop") + " " +
		m.style("inactive_fg").Render(Version)
	if m.opts.Host != "" {
		left += " " + m.style("hi_fg").Render(m.opts.Host)
	}
	right := m.style("main_fg").Render(m.now.Format(m.opts.ClockLayout))

	// The rule fills whatever is left, with one space of breathing room on
	// each side so the text never touches it.
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		return left + " " + right
	}
	rule := m.style("div_line").Render(strings.Repeat("─", gap))

	return left + " " + rule + " " + right
}

// denseProcess renders one rclone process as two lines: what it is, and what it
// is moving.
func (m Model) denseProcess(p model.Process, width int) string {
	label := strings.ToUpper(string(p.Kind))
	kindStyle := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color(boxColorFor(p.Kind)).Lipgloss()).
		Bold(true)

	// First line: what this process is acting on, both ends of it.
	head := kindStyle.Render(fmt.Sprintf("%-7s", label))
	head += m.renderPaths(p, width-lipgloss.Width(head))

	// Second line: identity and cost.
	meta := []string{
		m.style("inactive_fg").Render("pid ") + m.style("main_fg").Render(fmt.Sprint(p.PID)),
		m.style("inactive_fg").Render("up ") + m.style("main_fg").Render(Duration(p.Uptime())),
		m.style("inactive_fg").Render("rss ") + m.memStyle(p.RSS).Render(Bytes(p.RSS, m.opts.Base10)),
	}
	if p.Threads > 0 {
		meta = append(meta, m.style("inactive_fg").Render("thr ")+m.style("main_fg").Render(fmt.Sprint(p.Threads)))
	}
	second := "  " + strings.Join(meta, m.style("div_line").Render(" · "))

	// Third line: throughput. Read and write are graded along the download
	// and upload ramps, the same two colours btop uses for network traffic.
	var third string
	if p.IOAvailable {
		third = "  " +
			m.rateCell("↓", p.ReadRate, "download") +
			m.sparkline(m.graphs.read, p.PID, "download") +
			"  " + m.rateCell("↑", p.WriteRate, "upload") +
			m.sparkline(m.graphs.write, p.PID, "upload") +
			m.style("div_line").Render("  ·  ") +
			m.style("inactive_fg").Render("rd ") + m.style("main_fg").Render(Bytes(p.ReadTotal, m.opts.Base10)) +
			m.style("div_line").Render(" · ") +
			m.style("inactive_fg").Render("wr ") + m.style("main_fg").Render(Bytes(p.WriteTotal, m.opts.Base10))
	} else {
		// Saying so is the point: a zero here would be a lie, not a
		// measurement.
		third = "  " + m.style("inactive_fg").Render("throughput unavailable (process owned by another user)")
	}

	// Whatever the owning systemd unit and the job's own log recorded. Only
	// they know it, and the unit's own line is suppressed precisely because
	// this process line already says everything else about the same thing.
	//
	// The two sources are disjoint in practice rather than duplicated: a job
	// started with --log-file writes nothing to the journal, and one without it
	// has no log file to read.
	var errs []model.LogLine
	errs = append(errs, m.unitErrorsFor(p)...)
	errs = append(errs, m.jobErrorsFor(p)...)

	return head + "\n" + second + "\n" + third + "\n" +
		m.jobProgress(p) +
		m.renderErrors(errs, width)
}

// rateCell renders one direction of throughput, coloured by how close it is to
// the largest rate seen on this host.
func (m Model) rateCell(arrow string, bps float64, ramp string) string {
	scale := m.peakRate
	if scale < minRateScale {
		scale = minRateScale
	}
	st := m.gradientStyle(ramp, bps/scale)
	return st.Render(arrow+" ") +
		st.Render(fmt.Sprintf("%-*s", rateFieldWidth, Rate(bps, m.opts.Base10)))
}

// sparkline draws a process's recent throughput next to its current rate.
//
// The glyphs are coloured at the warm end of the same ramp that grades the
// number beside them, so the graph reads as belonging to that number rather
// than as a second, competing signal.
func (m Model) sparkline(rings map[int]*series.Ring, pid int, ramp string) string {
	s := m.graphs.spark(rings, pid, m.opts.GraphSymbol)
	if s == "" {
		return ""
	}
	return m.gradientStyle(ramp, 0.75).Render(s)
}

// memStyle grades resident memory along the "used" ramp, saturating at 1 GiB.
// rclone with a large VFS cache can genuinely climb, and that is worth seeing.
func (m Model) memStyle(rss uint64) lipgloss.Style {
	const saturate = 1 << 30
	return m.gradientStyle("used", float64(rss)/saturate)
}

// denseFooter summarises which collectors are alive, so an empty screen can
// always be explained.
func (m Model) denseFooter(width int) string {
	rule := m.style("div_line").Render(strings.Repeat("─", max(width, 1)))

	var parts []string
	sources := make([]string, 0, len(m.state.Seen))
	for s := range m.state.Seen {
		sources = append(sources, string(s))
	}
	sort.Strings(sources)
	for _, s := range sources {
		parts = append(parts, m.style("proc_misc").Render(s))
	}
	for src, err := range m.state.Errors {
		parts = append(parts, m.style("hi_fg").Render(fmt.Sprintf("%s: %v", src, err)))
	}
	if len(parts) == 0 {
		parts = append(parts, m.style("inactive_fg").Render("waiting for collectors"))
	}

	left := m.style("inactive_fg").Render("sources ") + strings.Join(parts, m.style("div_line").Render(" · "))
	right := m.style("inactive_fg").Render(fmt.Sprintf("%dms  q quit", m.opts.UpdateMS))

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return rule + "\n" + left + strings.Repeat(" ", gap) + right
}

// renderPaths draws the operands of a process, source first, joined by arrows.
//
// Remotes and local paths get different colours so the direction of a transfer
// is readable at a glance, and the available width is shared evenly between the
// operands rather than spent entirely on the first one.
func (m Model) renderPaths(p model.Process, room int) string {
	paths := p.Paths
	if len(paths) == 0 {
		paths = p.Remotes
	}
	if len(paths) == 0 {
		return ""
	}

	arrow := m.style("div_line").Render(" → ")
	budget := room - 3*(len(paths)-1)
	if budget < len(paths) {
		budget = len(paths)
	}
	per := budget / len(paths)

	parts := make([]string, 0, len(paths))
	for _, path := range paths {
		style := m.style("main_fg")
		if isRemote(path) {
			style = m.style("proc_misc")
		}
		parts = append(parts, style.Render(Truncate(shortenHome(path), per, true)))
	}
	return strings.Join(parts, arrow)
}

// isRemote reports whether a path is an rclone remote reference rather than a
// local path. A remote name cannot contain a slash, which is what separates
// "gdrive:Documents" from "/home/user/Documents".
func isRemote(path string) bool {
	i := strings.IndexByte(path, ':')
	return i > 0 && !strings.ContainsAny(path[:i], "/\\")
}

// shortenHome abbreviates the user's home directory, which otherwise eats a
// third of the width on every local path.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home) {
		return path
	}
	return "~" + path[len(home):]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
