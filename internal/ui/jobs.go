package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

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
	if job.Stats.Source == model.SourceRC {
		return m.statsProgress(job.Stats, "RC ")
	}
	return m.statsProgress(job.Stats, "")
}

// rcProgress renders asynchronous daemon jobs. Core statistics are merged into
// the process job by Resolve, so rendering them here would duplicate them.
func (m Model) rcProgress(stats *model.RCStats) string {
	if stats == nil {
		return ""
	}
	// Core statistics have already been merged into the process job by Resolve.
	// Keep the old rendering for hand-built legacy values, but do not print the
	// same measurement twice for an RC response carrying presence metadata.
	//
	// A value with neither presence bits nor a source is not a legacy
	// measurement at all -- it is a daemon that has answered only the
	// "transferring" key, and its zero Stats must stay silent rather than draw
	// "bytes 0 B / 0 B". The two are told apart by whether anything was
	// actually measured, not by the source alone.
	line := ""
	if stats.Stats.Known == 0 && stats.Stats.Source != model.SourceRC && stats.Stats.Measured() {
		line = m.statsProgress(stats.Stats, "RC ")
	}
	for _, job := range stats.Jobs {
		line += m.rcJobLine(job)
	}
	return line
}

// rcDaemonLine is what the daemon says about itself: which rclone it is, the
// resources it holds and any bandwidth limit it was given.
//
// Every group is drawn only when the daemon answered it. The endpoints behind
// them are separate calls that an older rclone may not implement, and the one
// rule this line has to keep is the same one the progress line keeps: an
// unanswered detail is unknown, never a zero. A bandwidth of "off" is a
// measurement -- rclone reports it as a negative rate -- and it must not read
// the same as a daemon that was never asked.
//
// The memory figure is "why is this mount eating RAM", which is the VFS cache
// most often; on the daemon's own line rather than the process's because it is
// the daemon, not the kernel process, that the number describes.
func (m Model) rcDaemonLine(stats *model.RCStats) string {
	if stats == nil {
		return ""
	}
	d := stats.Daemon
	var parts []string
	if d.Version != "" {
		parts = append(parts, m.label().Render("rclone ")+m.value().Render(d.Version))
	}
	if d.VFS.Answered {
		switch {
		case d.VFS.DiskCache && d.VFS.BytesSet:
			cache := m.label().Render("vfs cache ") +
				m.accentStyle(accentCacheSize).Render(Bytes(d.VFS.BytesUsed, m.opts.Base10))
			// The file count is its own measurement; a diskCache block that
			// named no file count must not be drawn as zero files.
			if d.VFS.FilesSet {
				cache += m.label().Render(fmt.Sprintf(" (%d files)", d.VFS.Files))
			}
			parts = append(parts, cache)
		case d.VFS.DiskCache:
			// A cache that answered without its size: there is one, but this
			// response did not say how big.
			parts = append(parts, m.label().Render("vfs cache"))
		default:
			// A measured "there is no cache" -- --vfs-cache-mode off -- which
			// is news, so it is not left out.
			parts = append(parts, m.label().Render("vfs cache off"))
		}
	}
	if d.Bandwidth.Known {
		limit := m.value().Render("off")
		if d.Bandwidth.BytesPerSecond >= 0 {
			limit = m.value().Render(Rate(float64(d.Bandwidth.BytesPerSecond), m.opts.Base10))
		}
		parts = append(parts, m.label().Render("bwlimit ")+limit)
	}
	// Each figure is drawn only if memstats supplied it. A response that carried
	// only Sys must not print "heap 0 B", which reads as a measured empty heap.
	if d.Memory.HeapAllocSet || d.Memory.SysSet {
		var mem []string
		if d.Memory.HeapAllocSet {
			mem = append(mem, m.label().Render("heap ")+
				m.value().Render(Bytes(d.Memory.HeapAlloc, m.opts.Base10)))
		}
		if d.Memory.SysSet {
			mem = append(mem, m.label().Render("sys ")+
				m.value().Render(Bytes(d.Memory.Sys, m.opts.Base10)))
		}
		parts = append(parts, strings.Join(mem, m.style("div_line").Render(" · ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, m.style("div_line").Render(" · ")) + "\n"
}

func (m Model) rcJobLine(job model.RCJob) string {
	state := "running"
	style := m.accentStyle(accentRunning)
	if job.Finished {
		if !job.SuccessKnown {
			state = "finished"
			style = m.style("inactive_fg")
		} else if job.Success {
			state = "successful"
		} else {
			state = "failed"
			style = m.alarm()
		}
	}
	line := "  " + m.label().Render("job ") + m.value().Render(fmt.Sprintf("#%d", job.ID)) +
		m.style("div_line").Render(" ") + style.Render(state)
	if job.Error != "" {
		line += m.style("div_line").Render(": ") + m.alarm().Render(oneLine(job.Error))
	}
	return line + "\n"
}

func (m Model) statsProgress(s model.JobStats, prefix string) string {

	var parts []string
	if prefix != "" {
		parts = append(parts, m.accentStyle(accentRunning).Render(prefix))
		if s.Known == 0 || s.Known&(model.StatsBytes|model.StatsTotalBytes) != 0 {
			bytes, total := Bytes(s.Bytes, m.opts.Base10), Bytes(s.TotalBytes, m.opts.Base10)
			if s.Known != 0 && s.Known&model.StatsBytes == 0 {
				bytes = "?"
			}
			if s.Known != 0 && s.Known&model.StatsTotalBytes == 0 {
				total = "?"
			}
			parts = append(parts, m.label().Render("bytes ")+m.value().Render(bytes)+
				m.style("div_line").Render(" / ")+m.label().Render(total))
		}
		if s.Known == 0 || s.Known&(model.StatsTransfers|model.StatsTotalTransfers) != 0 {
			transfers, total := fmt.Sprint(s.Transfers), fmt.Sprint(s.TotalTransfers)
			if s.Known != 0 && s.Known&model.StatsTransfers == 0 {
				transfers = "?"
			}
			if s.Known != 0 && s.Known&model.StatsTotalTransfers == 0 {
				total = "?"
			}
			parts = append(parts, m.label().Render("transfers ")+m.value().Render(transfers+"/"+total)+
				m.label().Render(" files"))
		}
		if s.Known == 0 || s.Known&model.StatsErrors != 0 {
			parts = append(parts, m.label().Render("errors ")+m.value().Render(fmt.Sprint(s.Errors)))
		}
		if s.Known == 0 || s.Known&model.StatsSpeed != 0 {
			parts = append(parts, m.label().Render("speed ")+m.value().Render(Rate(s.Speed, m.opts.Base10)))
		}
		if s.Known == 0 || s.Known&model.StatsElapsed != 0 {
			parts = append(parts, m.label().Render("elapsed ")+m.value().Render(Duration(s.Elapsed)))
		}
	}
	if frac, ok := s.Done(); ok {
		parts = append(parts,
			m.magnitudeStyle("cpu", frac).Render(fmt.Sprintf("%.0f%%", frac*100)))
	}
	if prefix == "" && (s.TotalBytes > 0 || s.Known&(model.StatsBytes|model.StatsTotalBytes) != 0) {
		bytes, total := Bytes(s.Bytes, m.opts.Base10), Bytes(s.TotalBytes, m.opts.Base10)
		if s.Known != 0 && s.Known&model.StatsBytes == 0 {
			bytes = "?"
		}
		if s.Known != 0 && s.Known&model.StatsTotalBytes == 0 {
			total = "?"
		}
		parts = append(parts,
			m.value().Render(bytes)+
				m.style("div_line").Render(" / ")+
				m.label().Render(total))
	}
	switch {
	case prefix != "":
		// The exact RC counters were already rendered above, including zeroes.
	case s.TotalTransfers > 0 || s.Known&(model.StatsTransfers|model.StatsTotalTransfers) != 0:
		transfers, total := fmt.Sprint(s.Transfers), fmt.Sprint(s.TotalTransfers)
		if s.Known != 0 && s.Known&model.StatsTransfers == 0 {
			transfers = "?"
		}
		if s.Known != 0 && s.Known&model.StatsTotalTransfers == 0 {
			total = "?"
		}
		parts = append(parts,
			m.value().Render(transfers+"/"+total)+
				m.label().Render(" files"))
	case s.Checks > 0 || s.Known&(model.StatsChecks|model.StatsTotalChecks) != 0:
		// A bisync with nothing to move is the healthy case, and it would look
		// idle if the only counters on the line were the transfers it did not
		// have to make.
		parts = append(parts,
			m.value().Render(fmt.Sprint(s.Checks))+
				m.label().Render(" checked"))
	}
	if prefix == "" && (s.Errors > 0 || s.Known&model.StatsErrors != 0) {
		errs := fmt.Sprintf("%d errors", s.Errors)
		if s.FatalError {
			errs += ", fatal"
		}
		parts = append(parts, m.alarm().Render(errs))
	}
	if prefix == "" && s.Known != 0 {
		if s.Known&model.StatsSpeed != 0 {
			parts = append(parts, m.label().Render("speed ")+m.value().Render(Rate(s.Speed, m.opts.Base10)))
		}
		if s.Known&model.StatsElapsed != 0 {
			parts = append(parts, m.label().Render("elapsed ")+m.value().Render(Duration(s.Elapsed)))
		}
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

// maxInFlight is how many files one job will name.
//
// Four is rclone's own default for --transfers, so an ordinary run is shown
// whole. Past that the list stops being one part of a summary and becomes the
// screen: sixteen parallel transfers would push the mounts, the pairs and the
// units off the bottom of the terminal, which is the one thing preset 0 exists
// not to do. What is cut is counted rather than dropped in silence, because
// four rows and no count read as a job with four files left in it.
const maxInFlight = 4

// minNameRoom is the narrowest a name may be squeezed before the whole list is
// dropped instead. A percentage and a rate with no name beside them say that
// something is moving without saying what, which the progress line above has
// already said better.
const minNameRoom = 8

// filesInFlight names what rclone is moving right now, one file to a row.
//
// These are the densest rows this program has, and the only per-file detail any
// collector can produce: /proc counts bytes through the kernel and knows
// nothing of files, systemd has the result and never the progress. They hang
// off the process they belong to, under the summary of its run.
func (m Model) filesInFlight(job model.Job, width int) string {
	// Nil and empty are different answers in the model -- nothing has said,
	// against nothing is moving -- and both draw nothing here. The progress
	// line above already accounts for the run, so a row announcing that no file
	// is in flight would be a second way of spelling an empty screen.
	if len(job.Transferring) == 0 {
		return ""
	}

	shown := job.Transferring
	if len(shown) > maxInFlight {
		shown = shown[:maxInFlight]
	}

	// Every row's figures are built before any row is drawn, because the name
	// field has to be one width for all of them. Budgeting each name against
	// its own row instead starts the figures at a different column on every
	// line, and four lines of ragged numbers read as four unrelated statements
	// rather than as a list -- which is most of what makes btop's screen
	// legible at a glance.
	prefix := "    " + m.style("div_line").Render("↳ ")

	// A column is shed before a row is. One file with a long estimate would
	// otherwise set the shared width for all of them and take the whole list
	// off a narrow terminal, when dropping the estimates leaves every name
	// legible -- and what is in flight is worth more than when it will land.
	figures, room := m.inFlightColumn(shown, width-lipgloss.Width(prefix), true)
	if room < minNameRoom {
		figures, room = m.inFlightColumn(shown, width-lipgloss.Width(prefix), false)
	}
	if room < minNameRoom {
		// Nothing but the name is left to cut, and a percentage with no name
		// beside it says that something is moving without saying what -- which
		// the progress line above has already said better.
		return ""
	}

	var b strings.Builder
	for i, t := range shown {
		// Cut from the left, like every other path here: the tail of a name
		// identifies the file and the head rarely does. rclone may have elided
		// the middle of it already, in a text log, and there is nothing to undo
		// that with.
		name := fmt.Sprintf("%-*s", room, Truncate(t.Name, room, true))
		b.WriteString(prefix + m.value().Render(name) + figures[i] + "\n")
	}
	if n := len(job.Transferring) - len(shown); n > 0 {
		// label(), not inactive_fg, and the difference from the identical-looking
		// count under an error list is the whole of the reservation: those are
		// entries that have already happened, these are files moving right now.
		// A live count dimmed to the colour that means "switched off" says the
		// opposite of what it counts.
		b.WriteString("    " +
			m.label().Render(fmt.Sprintf("and %d more transferring", n)) +
			"\n")
	}
	return b.String()
}

// inFlightColumn renders the figures for every row, and returns them with the
// width left over for the names: the narrowest of the rows, since they share
// one column.
func (m Model) inFlightColumn(ts []model.Transfer, avail int, withETA bool) ([]string, int) {
	figures := make([]string, len(ts))
	// Every row is measured against avail, which does not move. Subtracting
	// from the running minimum instead compounds the rows: three of them come
	// out three times as narrow as the widest one deserves, and the list
	// vanishes off a terminal with room for it.
	room := avail
	for i, t := range ts {
		figures[i] = m.inFlightFigures(t, withETA)
		if left := avail - lipgloss.Width(figures[i]); left < room {
			room = left
		}
	}
	return figures, room
}

// inFlightFigures is what is known about one file, as it sits to the right of
// its name.
func (m Model) inFlightFigures(t model.Transfer, withETA bool) string {
	parts := []string{
		m.transferDone(t),
		// The fraction is measured -- this file's rate over the largest the
		// host has shown -- so it is blended rather than indexed. The ramp is
		// chosen, and chosen without much to go on: a transfer record does not
		// say which way the bytes are moving, so this is the hue for "a file is
		// going somewhere" and not a claim about direction. An unmeasured rate
		// gets the same dash every other unknown here gets; Rate would clamp it
		// to zero and claim the file is stalled.
		m.inFlightSpeed(t),
	}
	if withETA && t.ETAKnown {
		// Same bargain as the run's own estimate: rclone writes "-" when it
		// cannot make one, and a zero would read as "any moment now".
		parts = append(parts, m.label().Render("ETA ")+m.value().Render(Duration(t.ETA)))
	}
	return "  " + strings.Join(parts, m.style("div_line").Render(" · "))
}

// inFlightSpeed renders one file's own rate, or the dash that means the record
// did not measure one.
func (m Model) inFlightSpeed(t model.Transfer) string {
	if t.Speed < 0 {
		return m.style("inactive_fg").Render("-")
	}
	return m.magnitudeStyle("upload", t.Speed/m.rateScale()).
		Render(Rate(t.Speed, m.opts.Base10))
}

// transferDone renders how far through one file rclone has got.
//
// The percentage is a measurement and takes the same ramp as the run's own
// completion on the line above, so a file and the job holding it are read the
// same way; a record that carried none gets the dash, because a zero would read
// as "not begun". The size beside it is what makes the percentage mean
// anything: 82% of 30 MiB and 82% of 30 GiB are different news.
func (m Model) transferDone(t model.Transfer) string {
	done := m.style("inactive_fg").Render("-")
	if t.Percentage >= 0 {
		done = m.magnitudeStyle("cpu", float64(t.Percentage)/100).
			Render(fmt.Sprintf("%d%%", t.Percentage))
	}
	if t.Size < 0 {
		// rclone could not size the source before it started. Zero would say
		// the file is empty and a bare percentage would be a fraction of
		// nothing, so the missing measurement is named as one.
		return done + m.label().Render(" of ") + m.style("inactive_fg").Render("unknown size")
	}
	return done + m.label().Render(" of ") +
		m.value().Render(Bytes(uint64(t.Size), m.opts.Base10))
}

// sideName is how one end of a bisync pair is identified on screen: the path
// the log spelled out, or failing that the mangled name on disk.
func sideName(s model.SyncSide) string {
	if s.Path != "" {
		return s.Path
	}
	return s.Label
}
