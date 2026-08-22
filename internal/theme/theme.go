// Package theme reads btop colour themes and turns them into styles.
//
// The file format is btop's, inherited from bashtop and bpytop: plain
// "theme[key]=\"#rrggbb\"" assignments. Reading it directly means rclonetop
// inherits every theme already installed for btop -- typically several dozen in
// /usr/share/btop/themes -- instead of shipping its own and looking like a
// different application on the same screen.
//
// The colour vocabulary maps onto rclone almost one to one: download and upload
// keep their meaning, the memory gradients (used, free, cached, available)
// describe remote and cache usage, the CPU gradient describes transfer
// throughput, and the process gradient colours the list of files in flight.
package theme

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// gradientSteps is the resolution btop generates its gradients at: 101 entries
// indexed 0..100. Matching it keeps the visual banding identical.
const gradientSteps = 101

// GradientNames are the colour ramps a theme provides, each defined by a
// _start, _mid and _end key.
var GradientNames = []string{
	"temp", "cpu", "free", "cached", "available", "used",
	"download", "upload", "process",
}

// assignment matches a single theme entry, tolerating the spacing and optional
// quoting seen across the themes in the wild.
//
// The value is captured as "everything up to the closing quote or whitespace"
// rather than as an alternation of shapes: an alternation whose first branch can
// match the empty string wins immediately under leftmost-first matching, which
// silently captures nothing and makes every theme look like the default.
// Validating the captured text is ParseHex's job.
var assignment = regexp.MustCompile(`^\s*theme\[([A-Za-z0-9_]+)\]\s*=\s*"?([^"\s]*)"?`)

// Color is an RGB colour that knows whether it was ever set. The distinction
// matters for the background: an unset background means "use the terminal's",
// which is how transparency works.
type Color struct {
	R, G, B uint8
	Set     bool
}

// Lipgloss converts to a lipgloss colour. Unset colours become NoColor, which
// leaves the terminal default in place.
func (c Color) Lipgloss() lipgloss.TerminalColor {
	if !c.Set {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B))
}

// Theme is a parsed colour scheme.
type Theme struct {
	Name      string
	colors    map[string]Color
	gradients map[string][]Color
}

// Color returns the colour for a key, falling back to the built-in default
// theme when a file omits it. Themes written for older btop versions are
// missing the newer keys, and a missing key must not render as black on black.
func (t *Theme) Color(key string) Color {
	if c, ok := t.colors[key]; ok && c.Set {
		return c
	}
	if t.Name != defaultName {
		return Default().Color(key)
	}
	return Color{}
}

// Gradient samples a colour ramp at frac, clamped to [0,1].
//
// This is what makes a value read at a glance: a transfer rate near the
// observed peak is coloured like a saturated CPU in btop, so the eye picks it
// out without reading the number.
func (t *Theme) Gradient(name string, frac float64) Color {
	ramp, ok := t.gradients[name]
	if !ok || len(ramp) == 0 {
		if t.Name != defaultName {
			return Default().Gradient(name, frac)
		}
		return Color{}
	}
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	return ramp[int(frac*float64(len(ramp)-1)+0.5)]
}

// SetOpaqueBackground controls whether the theme's own background is used.
// btop calls this theme_background; turning it off lets the terminal's
// background (and any transparency) show through.
func (t *Theme) SetOpaqueBackground(opaque bool) {
	if !opaque {
		t.colors["main_bg"] = Color{}
	}
}

// parse reads theme assignments from a reader.
func parse(name string, lines []string) *Theme {
	t := &Theme{
		Name:      name,
		colors:    make(map[string]Color, len(lines)),
		gradients: make(map[string][]Color, len(GradientNames)),
	}
	for _, line := range lines {
		m := assignment.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		c, err := ParseHex(m[2])
		if err != nil {
			continue // a malformed entry is skipped, not fatal
		}
		t.colors[m[1]] = c
	}
	t.buildGradients()
	return t
}

// buildGradients expands each start/mid/end triple into a 101-entry ramp,
// interpolating linearly in RGB exactly as btop does: when a mid colour is
// present the ramp is two 50-step halves, otherwise one 100-step run.
func (t *Theme) buildGradients() {
	for _, name := range GradientNames {
		start, hasStart := t.colors[name+"_start"]
		end, hasEnd := t.colors[name+"_end"]
		if !hasStart || !hasEnd || !start.Set || !end.Set {
			continue
		}
		mid, hasMid := t.colors[name+"_mid"]

		ramp := make([]Color, gradientSteps)
		if hasMid && mid.Set {
			half := gradientSteps / 2 // 50
			for i := 0; i <= half; i++ {
				ramp[i] = lerp(start, mid, float64(i)/float64(half))
			}
			for i := half; i < gradientSteps; i++ {
				ramp[i] = lerp(mid, end, float64(i-half)/float64(gradientSteps-1-half))
			}
		} else {
			for i := range ramp {
				ramp[i] = lerp(start, end, float64(i)/float64(gradientSteps-1))
			}
		}
		t.gradients[name] = ramp
	}
}

// lerp interpolates linearly between two colours in RGB space.
func lerp(a, b Color, f float64) Color {
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5)
	}
	return Color{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), Set: true}
}

// ParseHex reads a btop colour literal.
//
// Two forms exist. Six digits are a normal RGB triple. Two digits are a
// greyscale shorthand replicated across all three channels, which is how themes
// express the neutral greys ("#cc", "#40"). Three-digit CSS shorthand is not
// part of the format and is rejected, matching btop.
func ParseHex(s string) (Color, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Color{}, nil // unset: inherit from the terminal
	}
	if !strings.HasPrefix(s, "#") {
		return Color{}, fmt.Errorf("theme: %q is not a hex colour", s)
	}
	digits := s[1:]
	parse := func(sub string) uint8 {
		v, _ := strconv.ParseUint(sub, 16, 8)
		return uint8(v)
	}
	switch len(digits) {
	case 2:
		v := parse(digits)
		return Color{R: v, G: v, B: v, Set: true}, nil
	case 6:
		return Color{
			R:   parse(digits[0:2]),
			G:   parse(digits[2:4]),
			B:   parse(digits[4:6]),
			Set: true,
		}, nil
	default:
		return Color{}, fmt.Errorf("theme: invalid size of hex value %q", s)
	}
}

// SearchDirs lists the directories scanned for .theme files, in priority order.
// rclonetop's own directories come first so a user can override a btop theme of
// the same name, then btop's, so its installed collection is picked up as is.
func SearchDirs() []string {
	var dirs []string
	add := func(paths ...string) {
		for _, p := range paths {
			if p != "" {
				dirs = append(dirs, p)
			}
		}
	}

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		add(filepath.Join(xdg, "rclonetop", "themes"), filepath.Join(xdg, "btop", "themes"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(
			filepath.Join(home, ".config", "rclonetop", "themes"),
			filepath.Join(home, ".config", "btop", "themes"),
		)
	}
	add(
		"/usr/local/share/rclonetop/themes",
		"/usr/share/rclonetop/themes",
		"/usr/local/share/btop/themes",
		"/usr/share/btop/themes",
	)
	return dirs
}

// List returns the names of every theme found on disk, plus the built-in ones,
// deduplicated and sorted.
func List() []string {
	seen := map[string]bool{defaultName: true, ttyName: true}
	for _, dir := range SearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".theme" {
				continue
			}
			seen[strings.TrimSuffix(e.Name(), ".theme")] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Load finds and parses a theme by name. The two built-in names are handled
// without touching the filesystem.
func Load(name string) (*Theme, error) {
	switch name {
	case "", defaultName, "Default":
		return Default(), nil
	case ttyName, "TTY":
		return TTY(), nil
	}

	for _, dir := range SearchDirs() {
		path := filepath.Join(dir, name+".theme")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		defer f.Close()

		var lines []string
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("theme %s: %w", name, err)
		}
		return parse(name, lines), nil
	}
	return nil, fmt.Errorf("theme %q not found in %s", name, strings.Join(SearchDirs(), ", "))
}
