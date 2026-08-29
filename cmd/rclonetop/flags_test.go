package main

import (
	"testing"

	"github.com/Seykhel/rclonetop/internal/config"
)

func TestParseFlagsDefaults(t *testing.T) {
	o, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if o.updateMS != 2000 {
		t.Errorf("updateMS = %d, want 2000", o.updateMS)
	}
	if !o.themeBackground {
		t.Error("themeBackground should default to true")
	}
	if o.graphSymbol != "" {
		t.Errorf("graphSymbol = %q, want empty so the UI picks its own default", o.graphSymbol)
	}
	if o.preset != 0 {
		t.Errorf("preset = %d, want 0, the dense view", o.preset)
	}
}

func TestParseFlagsShortAndLongForms(t *testing.T) {
	// btop's short forms have to keep working alongside the long ones.
	for _, args := range [][]string{{"-u", "500"}, {"--update", "500"}} {
		o, err := parseFlags(args)
		if err != nil {
			t.Fatalf("parseFlags(%v): %v", args, err)
		}
		if o.updateMS != 500 {
			t.Errorf("parseFlags(%v): updateMS = %d, want 500", args, o.updateMS)
		}
	}
}

func TestParseFlagsRejectsBadValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		// Below this the UI spends more time redrawing than collecting.
		{"update too small", []string{"-u", "10"}},
		// A typo must not be silently ignored: the user would be left
		// wondering why the graphs did not change.
		{"unknown graph symbol", []string{"--graph-symbol", "brialle"}},
		{"unknown flag", []string{"--nonsense"}},
		// Only 0 and 1 are views that exist; anything else is a typo, not a
		// later version's value -- there is no runtime file to be forward
		// compatible with here.
		{"preset out of range", []string{"--preset", "2"}},
		{"preset not a number", []string{"--preset", "one"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseFlags(tt.args); err == nil {
				t.Errorf("parseFlags(%v) accepted the input", tt.args)
			}
		})
	}
}

func TestParseFlagsAcceptsEveryGraphSymbol(t *testing.T) {
	for _, sym := range []string{"braille", "block", "tty"} {
		o, err := parseFlags([]string{"--graph-symbol", sym})
		if err != nil {
			t.Errorf("parseFlags(--graph-symbol %s): %v", sym, err)
		}
		if o.graphSymbol != sym {
			t.Errorf("graphSymbol = %q, want %q", o.graphSymbol, sym)
		}
	}
}

func TestParseFlagsAcceptsEveryPreset(t *testing.T) {
	for _, args := range [][]string{{"-p", "1"}, {"--preset", "1"}} {
		o, err := parseFlags(args)
		if err != nil {
			t.Errorf("parseFlags(%v): %v", args, err)
		}
		if o.preset != 1 {
			t.Errorf("parseFlags(%v): preset = %d, want 1", args, o.preset)
		}
	}
}

func TestParseFlagsAcceptsConfigInBothForms(t *testing.T) {
	for _, args := range [][]string{{"-c", "/tmp/x.conf"}, {"--config", "/tmp/x.conf"}} {
		o, err := parseFlags(args)
		if err != nil {
			t.Fatalf("parseFlags(%v): %v", args, err)
		}
		if o.configPath != "/tmp/x.conf" {
			t.Errorf("parseFlags(%v): configPath = %q", args, o.configPath)
		}
		// Both spellings are one decision, recorded under the long form.
		if !o.explicit["config"] {
			t.Errorf("parseFlags(%v): the short form was not recorded as explicit", args)
		}
	}
}

func TestParseFlagsRecordsOnlyWhatWasTyped(t *testing.T) {
	o, err := parseFlags([]string{"-u", "500"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !o.explicit["update"] {
		t.Error("--update was typed but not recorded as explicit")
	}
	if o.explicit["theme"] {
		t.Error("--theme was never typed but is recorded as explicit")
	}
}

func TestApplyConfigFillsWhatTheCommandLineDidNotSay(t *testing.T) {
	o, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	cfg := config.Config{
		ColorTheme:      "dracula",
		ThemeBackground: false,
		GraphSymbol:     "block",
		UpdateMS:        500,
		Base10Sizes:     true,
		ForceTTY:        true,
		TrueColor:       false,
		ClockLayout:     "15:04",
	}
	o = applyConfig(o, cfg)

	if o.themeName != "dracula" {
		t.Errorf("themeName = %q, want dracula", o.themeName)
	}
	if o.themeBackground {
		t.Error("themeBackground = true, want the file's False")
	}
	if o.graphSymbol != "block" {
		t.Errorf("graphSymbol = %q, want block", o.graphSymbol)
	}
	if o.updateMS != 500 {
		t.Errorf("updateMS = %d, want 500", o.updateMS)
	}
	if !o.base10 {
		t.Error("base10 = false, want the file's True")
	}
	if !o.tty {
		t.Error("tty = false, want the file's force_tty")
	}
	// btop states this from the other side, and the key keeps btop's polarity.
	if !o.lowColor {
		t.Error("lowColor = false, want it set by truecolor = False")
	}
	if o.clockLayout != "15:04" {
		t.Errorf("clockLayout = %q, want 15:04", o.clockLayout)
	}
}

func TestParseFlagsDefaultsAgreeWithTheConfigDefaults(t *testing.T) {
	// The file is only ever laid underneath the command line, so a flag's own
	// default is what rclonetop does when neither source says anything -- and
	// that has to be the same number the file would have supplied. The two are
	// registered from one place; this is what says so out loud.
	o, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	d := config.Defaults()
	if o.themeName != d.ColorTheme {
		t.Errorf("themeName = %q, want the config default %q", o.themeName, d.ColorTheme)
	}
	if o.graphSymbol != d.GraphSymbol {
		t.Errorf("graphSymbol = %q, want the config default %q", o.graphSymbol, d.GraphSymbol)
	}
	if o.updateMS != d.UpdateMS {
		t.Errorf("updateMS = %d, want the config default %d", o.updateMS, d.UpdateMS)
	}
	if o.themeBackground != d.ThemeBackground {
		t.Errorf("themeBackground = %v, want the config default %v", o.themeBackground, d.ThemeBackground)
	}
	if o.base10 != d.Base10Sizes {
		t.Errorf("base10 = %v, want the config default %v", o.base10, d.Base10Sizes)
	}
	if o.tty != d.ForceTTY {
		t.Errorf("tty = %v, want the config default %v", o.tty, d.ForceTTY)
	}
	if o.lowColor != !d.TrueColor {
		t.Errorf("lowColor = %v, want the config default %v", o.lowColor, !d.TrueColor)
	}
	if o.clockLayout != d.ClockLayout {
		t.Errorf("clockLayout = %q, want the config default %q", o.clockLayout, d.ClockLayout)
	}
}

func TestGraphSymbolPrecedence(t *testing.T) {
	// Naming a symbol is a statement about the font and force_tty is one about
	// the terminal, so a named symbol wins -- but only against the same source.
	// Across sources the command line wins outright.
	tests := []struct {
		name string
		args []string
		cfg  func(config.Config) config.Config
		want string
	}{
		{
			name: "nobody has said anything",
			cfg:  func(c config.Config) config.Config { return c },
			want: "",
		},
		{
			name: "the file names a symbol",
			cfg:  func(c config.Config) config.Config { c.GraphSymbol = "block"; return c },
			want: "block",
		},
		{
			name: "the file forces tty",
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; return c },
			want: "tty",
		},
		{
			name: "a symbol named in the file beats force_tty in the file",
			cfg:  func(c config.Config) config.Config { c.GraphSymbol = "block"; c.ForceTTY = true; return c },
			want: "block",
		},
		{
			// The regression this function exists for. A default configuration
			// file naming braille would otherwise have taken ASCII graphs away
			// from every console user who copied one.
			name: "--tty beats a symbol named in the file",
			args: []string{"-t"},
			cfg:  func(c config.Config) config.Config { c.GraphSymbol = "braille"; return c },
			want: "tty",
		},
		{
			name: "--graph-symbol beats --tty",
			args: []string{"-t", "--graph-symbol", "block"},
			cfg:  func(c config.Config) config.Config { return c },
			want: "block",
		},
		{
			name: "--graph-symbol beats force_tty in the file",
			args: []string{"--graph-symbol", "braille"},
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; return c },
			want: "braille",
		},
		{
			// A boolean flag has three states, exactly as graph_symbol does:
			// not typed, typed true, typed false. flag.Visit reports only two
			// of them, so asking whether --tty was typed is not the same
			// question as asking whether TTY mode is on, and a rule that
			// confuses them turns --tty=false into --tty.
			name: "--tty=false leaves a symbol named in the file alone",
			args: []string{"--tty=false"},
			cfg:  func(c config.Config) config.Config { c.GraphSymbol = "block"; return c },
			want: "block",
		},
		{
			// The only way to say "no, this is not a console" at the prompt
			// when the file insists that it is -- so it had better work.
			name: "--tty=false overrules force_tty in the file",
			args: []string{"--tty=false"},
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; return c },
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseFlags(tt.args)
			if err != nil {
				t.Fatalf("parseFlags(%v): %v", tt.args, err)
			}
			o = applyConfig(o, tt.cfg(config.Defaults()))
			if o.graphSymbol != tt.want {
				t.Errorf("graphSymbol = %q, want %q", o.graphSymbol, tt.want)
			}
		})
	}
}

func TestApplyConfigDoesNotOverrideATypedFlag(t *testing.T) {
	// Including the case that makes the explicit map necessary in the first
	// place: --update 2000 is the same number as the built-in default, so
	// comparing values could not tell it from a flag that was never given, and
	// the file would overrule a user who typed exactly what they wanted.
	o, err := parseFlags([]string{"--update", "2000", "--theme", "gruvbox_dark"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	cfg := config.Defaults()
	cfg.UpdateMS = 500
	cfg.ColorTheme = "dracula"

	o = applyConfig(o, cfg)
	if o.updateMS != 2000 {
		t.Errorf("updateMS = %d: the file overruled a value typed on the command line", o.updateMS)
	}
	if o.themeName != "gruvbox_dark" {
		t.Errorf("themeName = %q: the file overruled a value typed on the command line", o.themeName)
	}
}

func TestExplicitKeysAreRealFlagNames(t *testing.T) {
	// A typo in one of these constants compiles, and switches off the
	// precedence rule for that option rather than failing: the configuration
	// file would simply start winning against a flag the user typed. Typing
	// every one of them and requiring it to be recorded is what catches a flag
	// renamed without its constant.
	keys := []string{
		flagTheme, flagGraphSymbol, flagUpdate,
		flagThemeBackground, flagBase10, flagTTY, flagLowColor,
	}
	args := []string{
		"--theme=dracula", "--graph-symbol=block", "--update=500",
		"--theme-background=false", "--base-10=true", "--tty=true", "--low-color=true",
	}
	if len(args) != len(keys) {
		t.Fatalf("%d flags typed for %d keys: they are meant to be the same list", len(args), len(keys))
	}

	o, err := parseFlags(args)
	if err != nil {
		t.Fatalf("parseFlags(%v): %v", args, err)
	}
	for _, k := range keys {
		if !o.explicit[k] {
			t.Errorf("%q is not the canonical name of any registered flag", k)
		}
	}
}

func TestTTYThemePrecedence(t *testing.T) {
	// The same rule as the graph symbol: naming a thing beats a flag that only
	// implies it. --tty says "this is a console", which answers the theme
	// question by implication; --theme answers it outright.
	tests := []struct {
		name string
		args []string
		cfg  func(config.Config) config.Config
		want bool
	}{
		{
			name: "nobody has said anything",
			cfg:  func(c config.Config) config.Config { return c },
			want: false,
		},
		{
			name: "--tty alone replaces the theme",
			args: []string{"-t"},
			cfg:  func(c config.Config) config.Config { return c },
			want: true,
		},
		{
			name: "a theme named at the prompt survives --tty",
			args: []string{"-t", "--theme", "dracula"},
			cfg:  func(c config.Config) config.Config { return c },
			want: false,
		},
		{
			name: "a theme named at the prompt survives force_tty in the file",
			args: []string{"--theme", "dracula"},
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; return c },
			want: false,
		},
		{
			// Not the exact parallel of the graph symbol, and deliberately so:
			// color_theme has no "unchosen" state, so the file cannot say
			// whether it meant its theme or merely holds the default.
			name: "force_tty in the file still replaces the file's own theme",
			args: nil,
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; c.ColorTheme = "dracula"; return c },
			want: true,
		},
		{
			name: "--tty=false replaces nothing",
			args: []string{"--tty=false"},
			cfg:  func(c config.Config) config.Config { c.ForceTTY = true; return c },
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := parseFlags(tt.args)
			if err != nil {
				t.Fatalf("parseFlags(%v): %v", tt.args, err)
			}
			o = applyConfig(o, tt.cfg(config.Defaults()))
			if o.ttyTheme != tt.want {
				t.Errorf("ttyTheme = %v, want %v", o.ttyTheme, tt.want)
			}
		})
	}
}

func TestApplyConfigLeavesTheGraphSymbolUnchosen(t *testing.T) {
	// Empty is a third state, not a fourth symbol. main reads it as "nobody has
	// chosen" and lets --tty supply plain ASCII; a default configuration file
	// naming braille here would have taken that away from every console user
	// who copied one.
	o, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	o = applyConfig(o, config.Defaults())
	if o.graphSymbol != "" {
		t.Errorf("graphSymbol = %q, want it left unchosen", o.graphSymbol)
	}
}
