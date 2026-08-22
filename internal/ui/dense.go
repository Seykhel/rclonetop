package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
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
	width := m.width
	if width <= 0 {
		width = 80
	}

	var b strings.Builder
	b.WriteString(m.denseHeader(width))
	b.WriteString("\n\n")

	procs := append([]model.Process(nil), m.state.Processes...)
	if len(procs) == 0 {
		b.WriteString(m.style("inactive_fg").Render("no rclone process running"))
		b.WriteString("\n")
	} else {
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

	b.WriteString("\n")
	b.WriteString(m.denseFooter(width))
	return b.String()
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
			"  " + m.rateCell("↑", p.WriteRate, "upload") +
			m.style("div_line").Render("  ·  ") +
			m.style("inactive_fg").Render("rd ") + m.style("main_fg").Render(Bytes(p.ReadTotal, m.opts.Base10)) +
			m.style("div_line").Render(" · ") +
			m.style("inactive_fg").Render("wr ") + m.style("main_fg").Render(Bytes(p.WriteTotal, m.opts.Base10))
	} else {
		// Saying so is the point: a zero here would be a lie, not a
		// measurement.
		third = "  " + m.style("inactive_fg").Render("throughput unavailable (process owned by another user)")
	}

	return head + "\n" + second + "\n" + third + "\n"
}

// rateCell renders one direction of throughput, coloured by how close it is to
// the largest rate seen on this host.
func (m Model) rateCell(arrow string, bps float64, ramp string) string {
	scale := m.peakRate
	if scale < minRateScale {
		scale = minRateScale
	}
	st := m.gradientStyle(ramp, bps/scale)
	return st.Render(arrow+" ") + st.Render(fmt.Sprintf("%-12s", Rate(bps, m.opts.Base10)))
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
