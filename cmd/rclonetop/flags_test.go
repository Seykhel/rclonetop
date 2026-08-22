package main

import "testing"

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
