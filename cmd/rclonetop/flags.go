package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Seykhel/rclonetop/internal/config"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// options holds the command line configuration.
//
// The short forms deliberately match btop's, so that muscle memory carries
// over: -u for the update rate, -t to force TTY mode, -l to limit the palette,
// -c to name a configuration file.
//
// Only flags that actually do something are registered. btop's -p and
// --vim-keys arrive with the box presets and with something to navigate;
// declaring them now would mean accepting a flag and silently ignoring it,
// which is worse than not accepting it at all.
type options struct {
	themeName       string
	graphSymbol     string
	themeBackground bool
	updateMS        int
	base10          bool
	tty             bool
	lowColor        bool
	debug           bool
	noAltScreen     bool
	configPath      string
	defaultConfig   bool
	showHelp        bool
	showVersion     bool

	// clockLayout has no flag of its own: a Go reference layout is too fiddly
	// to type at a prompt and too rarely changed to deserve one, so the
	// configuration file is the only way to set it.
	clockLayout string

	// explicit names the long form of every flag the user actually typed. It is
	// what applyConfig consults to decide whether a value came from the command
	// line or is merely the built-in default waiting to be overridden by the
	// configuration file.
	explicit map[string]bool
}

func parseFlags(args []string) (options, error) {
	var o options

	// Every flag that has a configuration key is registered with that key's
	// default rather than a literal of its own. The two have to agree -- the
	// file is only ever laid underneath the command line, so a flag's default
	// is what rclonetop does when neither says anything -- and taking them from
	// one place is what stops them drifting apart silently.
	d := config.Defaults()

	o.explicit = make(map[string]bool)
	o.clockLayout = d.ClockLayout // no flag of its own; see the field

	fs := flag.NewFlagSet("rclonetop", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // usage is printed by printUsage, not by flag

	// An option registered under both its short and its long form is two
	// entries in the flag set but one decision by the user, so each spelling
	// records the same canonical name -- the last one given, which is always the
	// long form.
	canonical := make(map[string]string)
	register := func(names []string, bind func(name string)) {
		for _, n := range names {
			bind(n)
			canonical[n] = names[len(names)-1]
		}
	}
	str := func(target *string, def string, names ...string) {
		register(names, func(n string) { fs.StringVar(target, n, def, "") })
	}
	num := func(target *int, def int, names ...string) {
		register(names, func(n string) { fs.IntVar(target, n, def, "") })
	}
	boolean := func(target *bool, def bool, names ...string) {
		register(names, func(n string) { fs.BoolVar(target, n, def, "") })
	}

	str(&o.themeName, d.ColorTheme, "theme")
	str(&o.graphSymbol, d.GraphSymbol, "graph-symbol")
	num(&o.updateMS, d.UpdateMS, "u", "update")
	boolean(&o.themeBackground, d.ThemeBackground, "theme-background")
	boolean(&o.base10, d.Base10Sizes, "base-10")
	boolean(&o.tty, d.ForceTTY, "t", "tty")
	boolean(&o.lowColor, !d.TrueColor, "l", "low-color")
	boolean(&o.debug, false, "d", "debug")
	boolean(&o.noAltScreen, false, "no-alt-screen")
	str(&o.configPath, "", "c", "config")
	boolean(&o.defaultConfig, false, "default-config")
	boolean(&o.showHelp, false, "h", "help")
	boolean(&o.showVersion, false, "V", "version")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			o.showHelp = true
			return o, nil
		}
		return o, err
	}
	// Visit reports only the flags that were seen on the command line, which is
	// exactly the question applyConfig has to answer. It cannot be answered by
	// comparing against the defaults instead: a user who types --update 2000 has
	// chosen 2000, and the configuration file must not overrule them.
	fs.Visit(func(f *flag.Flag) { o.explicit[canonical[f.Name]] = true })
	if o.updateMS < config.MinUpdateMS {
		return o, fmt.Errorf("--update must be at least %d ms, got %d", config.MinUpdateMS, o.updateMS)
	}
	// Plot itself falls back to braille on an unrecognised symbol, which is
	// what a configuration file written for a later version will want once
	// there is one. A typo typed at the prompt is just a mistake, though, and
	// ignoring it would leave the user wondering why nothing changed.
	switch o.graphSymbol {
	case "", "braille", "block", "tty":
	default:
		return o, fmt.Errorf("--graph-symbol must be braille, block or tty, got %q", o.graphSymbol)
	}
	return o, nil
}

// applyConfig lays the configuration file underneath the command line: a flag
// the user actually typed wins, and every other option takes the file's value.
//
// The precedence cannot be had for free by registering the file's values as the
// flag defaults, which is the usual trick, because the file to read is itself
// named by a flag. So the command line is parsed once against the built-in
// defaults and what the user typed is recorded as it goes, and the file is laid
// underneath afterwards.
//
// Nothing is re-validated here. The command line was checked as it was parsed,
// and the file was checked as it was read -- each against its own rules, which
// differ on purpose: a value from a later version's vocabulary is passed
// through by the file and refused at the prompt.
func applyConfig(o options, cfg config.Config) options {
	if !o.explicit["theme"] {
		o.themeName = cfg.ColorTheme
	}
	if !o.explicit["update"] {
		o.updateMS = cfg.UpdateMS
	}
	if !o.explicit["theme-background"] {
		o.themeBackground = cfg.ThemeBackground
	}
	if !o.explicit["base-10"] {
		o.base10 = cfg.Base10Sizes
	}
	if !o.explicit["tty"] {
		o.tty = cfg.ForceTTY
	}
	if !o.explicit["low-color"] {
		// btop states this one from the other side, and the key keeps btop's
		// polarity so that a btop.conf reads the same here.
		o.lowColor = !cfg.TrueColor
	}
	o.clockLayout = cfg.ClockLayout
	o.graphSymbol = resolveGraphSymbol(o, cfg)
	return o
}

// resolveGraphSymbol settles which glyphs the graphs are drawn from, where two
// settings can both have an opinion and disagree.
//
// Naming a symbol is a statement about the font; force_tty is a statement about
// the terminal, and it answers the font question only by implication. So a
// named symbol beats force_tty -- but only within the same source. Across
// sources the ordinary rule holds and the command line wins outright, because a
// flag typed now is a decision made now and the file's was made months ago.
//
// Getting this wrong is not visible in a test of either half alone: it was
// written first as "use ASCII when nothing has been named", which reads
// correctly until a default configuration file names braille, at which point
// --tty silently stops producing ASCII for everyone who copied one.
func resolveGraphSymbol(o options, cfg config.Config) string {
	switch {
	case o.explicit["graph-symbol"]:
		return o.graphSymbol
	case o.explicit["tty"]:
		return string(graph.TTY)
	case cfg.GraphSymbol != "":
		return cfg.GraphSymbol
	case cfg.ForceTTY:
		return string(graph.TTY)
	default:
		// Empty, still: the UI's own default is braille, and saying so here
		// would be a third place for that fact to live.
		return ""
	}
}

func printUsage(w io.Writer) {
	usage := strings.TrimLeft(`
rclonetop - a terminal monitor for rclone, in the style of btop++

Usage:
  rclonetop [options]

Options:
      --theme <name>      colour theme; btop themes are found automatically
      --graph-symbol <s>  graph glyphs: braille, block or tty
      --theme-background  use the theme's background colour (default true)
  -u, --update <ms>       refresh interval in milliseconds (default 2000)
      --base-10           size units in KB=1000 instead of KiB=1024
  -t, --tty               force TTY mode: 8 colours, and ASCII graphs unless
                          --graph-symbol says otherwise
  -l, --low-color         limit output to 256 colours
      --no-alt-screen     draw in place instead of on the alternate screen
  -c, --config <file>     read this configuration file instead of searching
      --default-config    print a commented default configuration, then exit
  -d, --debug             print what each collector saw, then exit
  -h, --help              show this message
  -V, --version           show the version

Keys:
  q, Esc                  quit
  +, -                    refresh faster or slower

Configuration is read from the first of these that exists, and anything given
on the command line overrides it:

  $XDG_CONFIG_HOME/rclonetop/rclonetop.conf
  ~/.config/rclonetop/rclonetop.conf

rclonetop never writes that file. To start from a documented one:

  rclonetop --default-config > ~/.config/rclonetop/rclonetop.conf

rclonetop reads only. It never changes rclone's configuration, starts or stops
transfers, or writes to any remote.
`, "\n")
	fmt.Fprint(w, usage)
}
