package main

import (
	"bytes"
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
