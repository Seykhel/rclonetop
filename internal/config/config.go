// Package config reads rclonetop's configuration file.
//
// The format is btop's, inherited in turn from bpytop and bashtop: comment
// lines introduced by "#", and "key = value" assignments whose booleans are
// spelled True and False. The key names are btop's too wherever the meaning is
// the same, so a setting anyone has already tuned there reads the same here.
//
// rclonetop never writes the file. btop rewrites its own configuration on exit,
// which rclonetop cannot do and stay read-only, so the commented default that
// btop would have written for you is printed by --default-config instead and it
// is the user who decides where -- and whether -- it lands.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Name is the file looked for in each of the search directories.
const Name = "rclonetop.conf"

// MinUpdateMS is the floor for the redraw interval, below which rclonetop
// spends more time redrawing than collecting. It is exported because the
// command line enforces the same floor, and two spellings of one number is how
// they come to disagree.
const MinUpdateMS = 100

// Config is the persisted configuration.
//
// Every field corresponds to an option that already does something. Keys for
// the box presets and the keybindings are deliberately absent rather than
// parsed and shelved: a key that is accepted and ignored is the same lie as a
// flag that is, and it is harder to notice in a file than at a prompt.
//
// It is a comparable struct on purpose -- no slices, no maps -- because that is
// what lets the round-trip test assert that --default-config prints exactly the
// defaults.
type Config struct {
	// ColorTheme names a .theme file or one of the two built-ins.
	ColorTheme string
	// ThemeBackground uses the theme's own background rather than the
	// terminal's, which is what lets terminal transparency show through.
	ThemeBackground bool
	// GraphSymbol selects the glyphs the sparklines are drawn from. Empty is a
	// third state, not a fourth symbol: it means nobody has chosen, which is
	// what lets force_tty supply plain ASCII without overriding a choice that
	// was made.
	GraphSymbol string
	// UpdateMS is the redraw interval in milliseconds.
	UpdateMS int
	// Base10Sizes reports sizes in KB=1000 rather than KiB=1024.
	Base10Sizes bool
	// ForceTTY assumes a Linux console: eight colours and ASCII graphs.
	ForceTTY bool
	// TrueColor is btop's spelling of the same choice --low-color makes from
	// the other side, and it keeps that polarity: false means the 256-colour
	// palette.
	TrueColor bool
	// ClockLayout is a Go reference layout for the clock in the header.
	//
	// btop spells this clock_format and means a strftime string. The key here
	// is deliberately not that name: btop's names are reused where the meaning
	// is the same, and a format nobody can share between the two is not the
	// same meaning.
	ClockLayout string
}

// Defaults is the configuration rclonetop uses when no file is found. It must
// agree with the flag defaults in cmd/rclonetop, and with what DefaultFile
// prints.
func Defaults() Config {
	return Config{
		ColorTheme:      "default",
		ThemeBackground: true,
		GraphSymbol:     "",
		UpdateMS:        2000,
		Base10Sizes:     false,
		ForceTTY:        false,
		TrueColor:       true,
		ClockLayout:     "15:04:05",
	}
}

// SearchPaths lists the configuration files consulted, in priority order. The
// order mirrors theme.SearchDirs so that both halves of "where does rclonetop
// look" have the same answer.
func SearchPaths() []string {
	var paths []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "rclonetop", Name))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".config", "rclonetop", Name))
	}
	return paths
}

// Load reads a configuration file.
//
// When path is empty the search directories are tried in turn and the first
// file that exists wins; finding none is the ordinary case and yields the
// defaults with no error. When path is given it is the file the user named on
// the command line, so it not existing is an error: falling back silently would
// hide a mistyped path behind a session in which none of their settings took.
func Load(path string) (Config, error) {
	if path != "" {
		return loadFile(path)
	}
	for _, p := range SearchPaths() {
		cfg, err := loadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return cfg, err
	}
	return Defaults(), nil
}

func loadFile(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Defaults(), err
	}
	defer f.Close()
	return parse(path, f)
}

// parse reads assignments from r over the top of the defaults.
//
// Two kinds of thing can be wrong with a line, and they are not treated alike.
// A key this build has never heard of is skipped: a file written for a later
// rclonetop will name boxes and presets that do not exist yet, and refusing to
// start on that would break every downgrade and every shared dotfile
// repository. A known key with a value that cannot mean anything -- a number
// that is not a number, a duration below the floor -- is an error naming the
// file and the line, because the user typed that and meant it.
func parse(name string, r io.Reader) (Config, error) {
	cfg := Defaults()

	sc := bufio.NewScanner(r)
	for line := 0; sc.Scan(); {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		// No trailing-comment syntax, matching btop. A "#" after the value is
		// part of the value, which is the safe reading: stripping it would
		// quietly truncate any setting that legitimately contains one.
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return cfg, fmt.Errorf("%s:%d: expected key = value, got %q", name, line, text)
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))

		fail := func(err error) (Config, error) {
			return cfg, fmt.Errorf("%s:%d: %s: %w", name, line, key, err)
		}

		switch key {
		case "color_theme":
			cfg.ColorTheme = value
		case "theme_background":
			b, err := parseBool(value)
			if err != nil {
				return fail(err)
			}
			cfg.ThemeBackground = b
		case "graph_symbol":
			// Not validated, unlike --graph-symbol: an unrecognised symbol
			// falls back to braille when it is plotted, so a value from a later
			// version costs a different-looking graph rather than a refusal to
			// start.
			cfg.GraphSymbol = value
		case "update_ms":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fail(fmt.Errorf("%q is not a number", value))
			}
			if n < MinUpdateMS {
				return fail(fmt.Errorf("must be at least %d, got %d", MinUpdateMS, n))
			}
			cfg.UpdateMS = n
		case "base_10_sizes":
			b, err := parseBool(value)
			if err != nil {
				return fail(err)
			}
			cfg.Base10Sizes = b
		case "force_tty":
			b, err := parseBool(value)
			if err != nil {
				return fail(err)
			}
			cfg.ForceTTY = b
		case "truecolor":
			b, err := parseBool(value)
			if err != nil {
				return fail(err)
			}
			cfg.TrueColor = b
		case "clock_layout":
			// The name differs from btop's clock_format because the value does:
			// Go formats a time by example rather than by strftime. Someone who
			// has both files open will still try "%X" here, and time.Format
			// would render it literally -- a header reading "%X" looks like a
			// bug in rclonetop rather than a mistake in the file -- so it is
			// refused with the reason.
			if strings.Contains(value, "%") {
				return fail(fmt.Errorf("takes a Go reference layout such as %q, not btop's strftime string", "15:04:05"))
			}
			cfg.ClockLayout = value
		case "clock_format":
			// The one deliberate exception to skipping keys this build does not
			// know. Every other unrecognised key might be a later version's;
			// this one never will be, because it is btop's name for a value
			// rclonetop cannot accept. Someone copying from btop.conf has made
			// a mistake that silence would leave them to find on screen, so the
			// rename is pointed out instead.
			return fail(fmt.Errorf("is btop's spelling; rclonetop calls it clock_layout and takes a Go reference layout such as %q", "15:04:05"))
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, fmt.Errorf("%s: %w", name, err)
	}
	return cfg, nil
}

// unquote strips one layer of double quotes, which btop writes around string
// values and a hand-edited file may well omit.
func unquote(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

// parseBool accepts btop's True and False in any case, because a file this
// application never rewrites is a file people edit by hand and the lowercase
// spelling is what every other configuration format uses. Nothing further:
// yes/no and 1/0 would be surface nobody asked for and nothing exercises.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("%q is not True or False", s)
}
