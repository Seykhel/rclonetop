package ui

import (
	"fmt"
	"time"
)

var (
	binaryUnits  = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	decimalUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}
)

// Bytes formats a byte count. base10 selects KB=1000 over KiB=1024, matching
// btop's base_10_sizes option and rclone's own --human-readable conventions.
func Bytes(n uint64, base10 bool) string {
	return scaled(float64(n), base10, "")
}

// Rate formats a throughput in bytes per second.
func Rate(bps float64, base10 bool) string {
	if bps < 0 {
		bps = 0
	}
	return scaled(bps, base10, "/s")
}

func scaled(v float64, base10 bool, suffix string) string {
	div, units := 1024.0, binaryUnits
	if base10 {
		div, units = 1000.0, decimalUnits
	}

	i := 0
	for v >= div && i < len(units)-1 {
		v /= div
		i++
	}

	// Bytes are always whole; larger units get one decimal below 10 so the
	// column stays narrow without losing resolution where it matters.
	switch {
	case i == 0:
		return fmt.Sprintf("%.0f %s%s", v, units[i], suffix)
	case v < 10:
		return fmt.Sprintf("%.1f %s%s", v, units[i], suffix)
	default:
		return fmt.Sprintf("%.0f %s%s", v, units[i], suffix)
	}
}

// Duration formats an uptime compactly: the two most significant units, which
// is enough to answer "is this fresh or stale" without the noise of seconds on
// a process that has been up for days.
func Duration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Second)

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm%ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// Ago formats how long ago an instant was.
//
// A gap below one second reads as "just now" rather than borrowing Duration's
// dash, which stands for "unknown" and would misdescribe a measurement that was
// in fact taken a moment ago.
func Ago(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	return Duration(d) + " ago"
}

// Truncate shortens s to width, marking the cut with an ellipsis. Paths are cut
// from the left, because the tail of a path identifies it and the head rarely
// does.
//
// A width of zero or less yields nothing. Returning the whole string there --
// as this did -- turns every arithmetic slip in a caller's budget into an
// overflowing line, silently and only on narrow terminals: a journal message
// rendered at a computed width of -5 came out at a hundred and sixty columns.
func Truncate(s string, width int, fromLeft bool) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	if fromLeft {
		return "…" + string(r[len(r)-width+1:])
	}
	return string(r[:width-1]) + "…"
}
