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
// -c to name a configuration file, -p to start on a given view.
//
// Only flags that actually do something are registered. btop's -p named a
// saved box arrangement and stayed unregistered until there was one to name;
// -p here is a narrower question -- which of the two views that already exist
// to start on -- and #11 answers it once two real views exist, ahead of #7's
// larger box-preset system. --vim-keys still has nothing on screen to move
// between, so it stays out: declaring it now would mean accepting a flag and
// silently ignoring it, which is worse than not accepting it at all.
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

	// preset is the view/layout to start in: 0 is dense and 1-9 are framed
	// arrangements. The arrangements themselves come from the configuration.
	preset  int
	presets [10]string

	// shownBoxes has no flag of its own -- unlike clockLayout, not because
	// it is fiddly to type, but because it names runtime UI state: the
	// framed view's own digit keys are what change it during a session, and
	// a flag would only duplicate that for the rare case of starting with
	// something already hidden.
	shownBoxes string

	// ttyTheme is the answer to "should the eight-colour built-in theme replace
	// whatever was named", which --tty and --theme can both speak to. Settled
	// by applyConfig for the same reason graphSymbol is: that is the last place
	// it is still known who asked for what.
	ttyTheme bool

	// explicit names the long form of every flag the user actually typed. It is
	// what applyConfig consults to decide whether a value came from the command
	// line or is merely the built-in default waiting to be overridden by the
	// configuration file.
	//
	// It answers "was this typed", which for a boolean is not the same question
	// as "is this on": --tty=false is typed and off. Anything settling a rule
	// between two options has to ask both.
	explicit map[string]bool
}

// The long forms the explicit map is keyed by. They are constants because a
// typo in one of these strings compiles, passes every test that happens not to
// cover that option, and silently switches off the precedence rule for it --
// the configuration file would just start winning against a typed flag, which
// is the one thing this whole mechanism exists to prevent.
const (
	flagTheme           = "theme"
	flagGraphSymbol     = "graph-symbol"
	flagUpdate          = "update"
	flagThemeBackground = "theme-background"
	flagBase10          = "base-10"
	flagTTY             = "tty"
	flagLowColor        = "low-color"
)

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

	str(&o.themeName, d.ColorTheme, flagTheme)
	str(&o.graphSymbol, d.GraphSymbol, flagGraphSymbol)
	num(&o.updateMS, d.UpdateMS, "u", flagUpdate)
	boolean(&o.themeBackground, d.ThemeBackground, flagThemeBackground)
	boolean(&o.base10, d.Base10Sizes, flagBase10)
	boolean(&o.tty, d.ForceTTY, "t", flagTTY)
	boolean(&o.lowColor, !d.TrueColor, "l", flagLowColor)
	// No configuration key -- see the field.
	num(&o.preset, 0, "p", "preset")
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
	// Unlike graph_symbol in the file, there is no runtime fallback to lean
	// on here for a value typed at the prompt: a typo wants an answer now,
	// not a silent drop to preset 0.
	if o.preset < 0 || o.preset > 9 {
		return o, fmt.Errorf("--preset must be 0 through 9, got %d", o.preset)
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
	if !o.explicit[flagTheme] {
		o.themeName = cfg.ColorTheme
	}
	if !o.explicit[flagUpdate] {
		o.updateMS = cfg.UpdateMS
	}
	if !o.explicit[flagThemeBackground] {
		o.themeBackground = cfg.ThemeBackground
	}
	if !o.explicit[flagBase10] {
		o.base10 = cfg.Base10Sizes
	}
	if !o.explicit[flagTTY] {
		o.tty = cfg.ForceTTY
	}
	if !o.explicit[flagLowColor] {
		// btop states this one from the other side, and the key keeps btop's
		// polarity so that a btop.conf reads the same here.
		o.lowColor = !cfg.TrueColor
	}
	o.clockLayout = cfg.ClockLayout
	o.shownBoxes = cfg.ShownBoxes
	o.presets = cfg.Presets()

	// Both of these read o.tty, so they have to run after it has been settled
	// just above -- they want the resolved answer to "is this a console", not
	// the file's opinion of it. Moving either call up would compile and would
	// quietly start ignoring --tty=false.
	o.graphSymbol = resolveGraphSymbol(o, cfg)
	o.ttyTheme = resolveTTYTheme(o)
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
// Getting this wrong is not visible in a test of either half alone. It was
// written first as "use ASCII when nothing has been named", which reads
// correctly until a default configuration file names braille, at which point
// --tty silently stops producing ASCII for everyone who copied one. The second
// attempt asked o.explicit[flagTTY] on its own, which reads correctly until
// somebody types --tty=false -- a flag that was typed, and is off. Hence both
// halves of the second case, and o.tty rather than cfg.ForceTTY in the fourth:
// the file's opinion has already lost to the prompt by then, and asking it
// again would let it win after all.
func resolveGraphSymbol(o options, cfg config.Config) string {
	switch {
	case o.explicit[flagGraphSymbol]:
		return o.graphSymbol
	case o.explicit[flagTTY] && o.tty:
		return string(graph.TTY)
	case cfg.GraphSymbol != "":
		return cfg.GraphSymbol
	case o.tty:
		return string(graph.TTY)
	default:
		// Empty, still: the UI's own default is braille, and saying so here
		// would be a third place for that fact to live.
		return ""
	}
}

// resolveTTYTheme decides whether TTY mode replaces the theme that was named.
//
// The same rule as the graph symbol, one step on: naming a thing beats a flag
// that only implies it. --tty says "this is a console", which answers the theme
// question by implication; --theme answers it outright. So a theme named at the
// prompt survives --tty, and only the colour depth and the glyphs are forced.
//
// The parallel is not exact on the file's side, and the reason is worth knowing
// before anyone tries to make it exact: graph_symbol has a third state that
// says "unchosen", so the file can distinguish a symbol it named from one it
// merely defaulted to. color_theme has no such state -- it always holds
// something, and "default" is a theme somebody may have chosen on purpose. The
// file therefore cannot say whether it meant its theme, so force_tty read from
// a file still replaces it. Only the prompt can be sure.
func resolveTTYTheme(o options) bool {
	return o.tty && !o.explicit[flagTheme]
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
	  -p, --preset <0..9>     view/layout to start in: 0 dense, 1-9 framed
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
