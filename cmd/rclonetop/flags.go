package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// options holds the command line configuration.
//
// The short forms deliberately match btop's, so that muscle memory carries
// over: -u for the update rate, -t to force TTY mode, -l to limit the palette.
//
// Only flags that actually do something are registered. btop's -c, -p and
// --vim-keys arrive with the configuration file and the box presets; declaring
// them now would mean accepting a flag and silently ignoring it, which is worse
// than not accepting it at all.
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
	showHelp        bool
	showVersion     bool
}

func parseFlags(args []string) (options, error) {
	var o options

	fs := flag.NewFlagSet("rclonetop", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // usage is printed by printUsage, not by flag

	str := func(target *string, def string, names ...string) {
		for _, n := range names {
			fs.StringVar(target, n, def, "")
		}
	}
	num := func(target *int, def int, names ...string) {
		for _, n := range names {
			fs.IntVar(target, n, def, "")
		}
	}
	boolean := func(target *bool, def bool, names ...string) {
		for _, n := range names {
			fs.BoolVar(target, n, def, "")
		}
	}

	str(&o.themeName, "", "theme")
	str(&o.graphSymbol, "", "graph-symbol")
	num(&o.updateMS, 2000, "u", "update")
	boolean(&o.themeBackground, true, "theme-background")
	boolean(&o.base10, false, "base-10")
	boolean(&o.tty, false, "t", "tty")
	boolean(&o.lowColor, false, "l", "low-color")
	boolean(&o.debug, false, "d", "debug")
	boolean(&o.noAltScreen, false, "no-alt-screen")
	boolean(&o.showHelp, false, "h", "help")
	boolean(&o.showVersion, false, "V", "version")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			o.showHelp = true
			return o, nil
		}
		return o, err
	}
	if o.updateMS < 100 {
		return o, fmt.Errorf("--update must be at least 100 ms, got %d", o.updateMS)
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
  -d, --debug             print what each collector saw, then exit
  -h, --help              show this message
  -V, --version           show the version

Keys:
  q, Esc                  quit
  +, -                    refresh faster or slower

rclonetop reads only. It never changes rclone's configuration, starts or stops
transfers, or writes to any remote.
`, "\n")
	fmt.Fprint(w, usage)
}
