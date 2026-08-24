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
