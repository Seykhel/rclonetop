package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muesli/termenv"

	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

func TestWriteDisplayAnswersTheColourQuestion(t *testing.T) {
	// "The colours look washed out" has three candidate causes and a screenshot
	// tells them apart from none of them. All three have to be in the dump, or
	// the next investigation is an argument rather than a reading.
	var b bytes.Buffer
	writeDisplay(&b, display{
		Profile:   termenv.ANSI256,
		Theme:     "dracula",
		Symbol:    graph.Braille,
		Term:      "screen-256color",
		ColorTerm: "",
	})

	out := b.String()
	for _, want := range []string{"256 colours", "dracula", "braille", "screen-256color", "COLORTERM"} {
		if !strings.Contains(out, want) {
			t.Errorf("the display block never mentions %q:\n%s", want, out)
		}
	}
}

func TestWriteDisplaySaysWhenTheProfileIsThePipesAndNotTheScreens(t *testing.T) {
	// Users are asked to paste -d into a bug report, so they redirect it, so
	// termenv sees a pipe and correctly reports no colour. Printing that bare to
	// somebody whose complaint is that the colours look wrong would answer the
	// wrong question with total confidence.
	var piped, onATerminal bytes.Buffer
	writeDisplay(&piped, display{Profile: termenv.Ascii, Terminal: false})
	writeDisplay(&onATerminal, display{Profile: termenv.TrueColor, Terminal: true})

	if !strings.Contains(piped.String(), "not a terminal") {
		t.Errorf("a piped dump does not say the profile is the pipe's:\n%s", piped.String())
	}
	if strings.Contains(onATerminal.String(), "not a terminal") {
		t.Errorf("a dump straight to a terminal carries the caveat anyway:\n%s", onATerminal.String())
	}
}

func TestDevNullIsNotATerminal(t *testing.T) {
	// The case that broke the first version of this. isTerminal asked the mode
	// bits -- "is this a character device" -- which is true of a tty and false
	// of a pipe and of a regular file, and *also true of /dev/null*. So
	// `rclonetop -d > /dev/null` reported a terminal and suppressed the caveat
	// that the answer exists to carry.
	//
	// Nobody reads /dev/null, so the practical cost was nil. The reasoning
	// written above the check was what actually failed, and this repo holds that
	// a comment explaining why is load-bearing: a false rationale is worse than
	// no rationale, because the next reader believes it.
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("cannot open %s: %v", os.DevNull, err)
	}
	defer f.Close()

	if isTerminal(f) {
		t.Errorf("%s reports as a terminal: the check is looking at the mode bits again", os.DevNull)
	}
}

func TestARegularFileIsNotATerminal(t *testing.T) {
	// The ordinary shape of a bug report: `rclonetop -d > dump.txt`.
	f, err := os.Create(filepath.Join(t.TempDir(), "dump.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if isTerminal(f) {
		t.Error("a regular file reports as a terminal")
	}
}

func TestSymbolNameSpellsOutTheUnchosenCase(t *testing.T) {
	// Empty is the third state, not a missing value, and %q would print it as
	// one -- in the file whose whole job is to be read by somebody puzzled.
	if got := symbolName(""); !strings.Contains(got, "braille") || !strings.Contains(got, "default") {
		t.Errorf("symbolName(\"\") = %q, want it to name the default and say it is one", got)
	}
	if got := symbolName(graph.Block); !strings.Contains(got, "block") {
		t.Errorf("symbolName(block) = %q", got)
	}
}

func TestProfileNameSaysWhatEachProfileMeans(t *testing.T) {
	// The constant names alone are not an answer: "Ascii" reads as a glyph
	// problem rather than as "this terminal was given no colour at all".
	for _, tt := range []struct {
		profile termenv.Profile
		want    string
	}{
		{termenv.TrueColor, "truecolor"},
		{termenv.ANSI256, "quantised"},
		{termenv.ANSI, "8 colours"},
		{termenv.Ascii, "monochrome"},
	} {
		if got := profileName(tt.profile); !strings.Contains(got, tt.want) {
			t.Errorf("profileName(%v) = %q, want it to mention %q", tt.profile, got, tt.want)
		}
	}
}
