package ui

import (
	"testing"

	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

func themes() map[string]*theme.Theme {
	return map[string]*theme.Theme{"default": theme.Default(), "tty": theme.TTY()}
}

func TestTextIsNeverPaintedInARampsDarkEnd(t *testing.T) {
	// The regression this whole change exists for. btop's ramps begin dark on
	// purpose -- download_start is #291f75 -- because btop only ever uses that
	// end as a *filled cell*, where dark reads as "not much". Indexing the same
	// ramp to colour letters wrote "↓ 0 B/s" in near-black violet, and an idle
	// mount reports exactly zero for hours at a time.
	for name, th := range themes() {
		m := New(nil, Options{Theme: th}, nil)
		main := th.Color("main_fg")

		for _, ramp := range theme.GradientNames {
			if got := m.magnitudeColor(ramp, 0); got != main {
				t.Errorf("%s: %s at zero = %+v, want main_fg %+v", name, ramp, got, main)
			}
		}
	}
}

func TestTextStaysLegibleAcrossTheWholeRamp(t *testing.T) {
	// Zero is only where it was worst. A rate at three per cent of the observed
	// peak is still a measurement somebody is trying to read, so the guarantee
	// has to hold over the low end generally rather than at one point.
	//
	// Half of main_fg's own brightness is the floor: dimmer than the body text
	// by a visible margin, which is the point of grading it at all, but nowhere
	// near the background. The old behaviour scored 39 against a floor of 102.
	for name, th := range themes() {
		m := New(nil, Options{Theme: th}, nil)
		floor := luminance(th.Color("main_fg")) / 2

		for _, ramp := range theme.GradientNames {
			for _, frac := range []float64{0, 0.03, 0.1, 0.25, 0.4, 0.5} {
				got := luminance(m.magnitudeColor(ramp, frac))
				if got < floor {
					t.Errorf("%s: %s at %.2f has luminance %.0f, below the floor of %.0f",
						name, ramp, frac, got, floor)
				}
			}
		}
	}
}

func TestTheRawRampReallyIsTooDarkForText(t *testing.T) {
	// The premise the two tests above are the cure for. Without this one they
	// prove only that the new arithmetic is bright, not that the old was dark,
	// and someone could "simplify" magnitudeColor back into a plain Gradient
	// call with every other test still green.
	//
	// This is not a complaint about the theme. gradientStyle is still correct
	// for the sparkline, because a dark braille cell against a dark background
	// reads as "not much" -- which is true, and is what btop designed the ramp
	// ends for. It is only letters that need the floor.
	th := theme.Default()
	floor := luminance(th.Color("main_fg")) / 2

	if got := luminance(th.Gradient("download", 0)); got >= floor {
		t.Errorf("download_start has luminance %.0f, at or above the floor of %.0f: "+
			"either the theme changed or the floor is too low to mean anything", got, floor)
	}
}

func TestMagnitudeStillReachesTheRampsHotEnd(t *testing.T) {
	// Legibility must not be bought by flattening the signal: a saturated link
	// has to look saturated, or the grading says nothing.
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)

	for _, ramp := range theme.GradientNames {
		want := th.Gradient(ramp, 1)
		if got := m.magnitudeColor(ramp, 1); got != want {
			t.Errorf("%s at one = %+v, want the ramp's own end %+v", ramp, got, want)
		}
	}
}

func TestMagnitudeClampsFractionsOutsideTheUnitInterval(t *testing.T) {
	// A rate divided by the peak seen so far is exactly 1.0 on the sample that
	// sets a new peak, and floating point makes "exactly" optimistic. Gradient
	// clamps its own index, but Blend does not clamp the mixing fraction, so
	// without a clamp here the arithmetic would run past the ramp's end.
	m := New(nil, Options{}, nil)

	if got, want := m.magnitudeColor("download", 1.5), m.magnitudeColor("download", 1); got != want {
		t.Errorf("above one = %+v, want it clamped to %+v", got, want)
	}
	if got, want := m.magnitudeColor("download", -0.5), m.magnitudeColor("download", 0); got != want {
		t.Errorf("below zero = %+v, want it clamped to %+v", got, want)
	}
}

func TestFixedAccentsAreLegible(t *testing.T) {
	// The other half of the rule. A point somebody chose is indexed rather than
	// blended, because blending only dilutes a colour already picked for being
	// legible -- that is what turned the cache figures from saturated cyan into
	// a pale wash. Nothing then guarantees the chosen point is a good one, so it
	// is guaranteed here instead.
	//
	// The built-in theme only. The eight-colour theme's ramps are saturated
	// primaries, and Rec. 709 scores #ff0000 at 54 while a console renders it at
	// full intensity and perfectly readable: luminance is a poor proxy for a pure
	// hue, and there is no blend towards the foreground to rescue one here.
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)
	floor := luminance(th.Color("main_fg")) / 2

	for _, a := range textAccents {
		if got := luminance(m.accentColor(a)); got < floor {
			t.Errorf("the %s ramp at %.2f has luminance %.0f, below the floor of %.0f: "+
				"pick a brighter point on it, or blend it instead",
				a.ramp, a.at, got, floor)
		}
	}
}

func TestALabelIsDimmerThanItsValueAndBrighterThanInert(t *testing.T) {
	// Three levels, and each boundary was a mistake on one side of it. Labels
	// were inactive_fg -- #40, the colour that means "switched off" -- which made
	// most of the screen invisible and left nothing to say "switched off" with.
	// Made main_fg instead, they became indistinguishable from the figures they
	// name. Halfway is legible and plainly secondary, which is all a label is.
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)

	inert := luminance(th.Color("inactive_fg"))
	label := luminance(m.labelColor())
	value := luminance(th.Color("main_fg"))

	if !(inert < label && label < value) {
		t.Errorf("luminance should climb inert < label < value, got %.0f, %.0f, %.0f",
			inert, label, value)
	}
	if label < value/2 {
		t.Errorf("a label at %.0f is below half the body text at %.0f, which is where the last one was unreadable",
			label, value)
	}
	// Bold rides with colour and only with colour: emphasising every figure
	// emphasises none of them, which is what the first attempt did.
	if m.value().GetBold() {
		t.Error("a plain value is bold; bold belongs to the styles that carry a magnitude")
	}
}

func TestWeightIsBuiltIntoTheColouredStyles(t *testing.T) {
	// "Bold rides with colour and only with colour" was a sentence in CLAUDE.md
	// and a .Bold(true) repeated at fourteen call sites, none of which wanted it
	// otherwise. A rule with no exceptions belongs in the constructor, where the
	// fifteenth caller cannot forget it -- and this is what says so, so that
	// nobody "tidies" the weight back out into the callers.
	m := New(nil, Options{}, nil)

	if !m.magnitudeStyle("download", 0.5).GetBold() {
		t.Error("magnitudeStyle is not bold on its own")
	}
	if !m.accentStyle(accentCacheSize).GetBold() {
		t.Error("accentStyle is not bold on its own")
	}
	if !m.alarm().GetBold() {
		t.Error("alarm is not bold on its own")
	}
	// And the area style is not, because area carries no weight -- a braille
	// cell has no glyph shape to thicken, only a colour.
	if m.gradientStyle("download", 0.75).GetBold() {
		t.Error("gradientStyle is bold; it fills area, where weight means nothing")
	}
}

// A meter's cell is not against the background, it is against meter_bg, and the
// cold end of most ramps is darker than that. A bar at four per cent showed a
// hole rather than a mark: the failure the text rule exists to prevent, arrived
// from the other side.
func TestEveryFilledCellClearsTheTrack(t *testing.T) {
	// The tty palette is exempt for the reason the accent test exempts it:
	// eight saturated colours whose Rec. 709 luminance lies about them. That
	// mode tells the two halves apart by shape instead.
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)
	want := luminance(th.Color("meter_bg")) + meterFloor

	for _, ramp := range theme.GradientNames {
		for _, at := range []float64{0, 0.05, 0.2, 0.5, 0.8, 1} {
			if got := luminance(m.meterColor(ramp, at)); got < want {
				t.Errorf("%s at %.2f has luminance %.0f, below the track's floor of %.0f",
					ramp, at, got, want)
			}
		}
	}
}

func TestTheRampsColdEndReallyIsLostAgainstTheTrack(t *testing.T) {
	// The premise the test above is the cure for, pinned so the cure cannot be
	// "simplified" back into a plain gradientColor call with everything else
	// still green. Five of the nine ramps start below the track; these two are
	// the worst, and they are the ones a bandwidth meter would use.
	th := theme.Default()
	track := luminance(th.Color("meter_bg"))

	for _, ramp := range []string{"download", "upload"} {
		if got := luminance(th.Gradient(ramp, 0)); got >= track {
			t.Errorf("%s_start has luminance %.0f, already at or above the track's %.0f: "+
				"either the theme changed or this no longer measures anything", ramp, got, track)
		}
	}
}

// And the lift is paid for only where it is needed: a ramp that was already
// clear of the track keeps its own colours, the hot end included, or the bar
// stops saying anything about magnitude.
func TestTheLiftLeavesALegibleRampAlone(t *testing.T) {
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)

	for _, ramp := range theme.GradientNames {
		if got, want := m.meterColor(ramp, 1), th.Gradient(ramp, 1); got != want {
			t.Errorf("%s at one = %+v, want the ramp's own end %+v", ramp, got, want)
		}
	}
	// cpu starts bright and needs no help anywhere.
	for _, at := range []float64{0, 0.5, 1} {
		if got, want := m.meterColor("cpu", at), th.Gradient("cpu", at); got != want {
			t.Errorf("cpu at %.1f was lifted from %+v to %+v for no reason", at, want, got)
		}
	}
}

// btop grades a graph up its own height rather than along the value: the top
// row is the ramp's hot end whatever the reading, so a peak that reaches it says
// "this is as busy as this process has been" by colour alone.
//
// Raw, and this is the one place the measurement says raw is right. A graph row
// sits against main_bg, which is black in the built-in theme; the coldest ramp
// end is upload at luminance 32, faint but present. The meter needed lifting
// because meter_bg is 64 -- lighter than five of the ramps' cold ends -- and a
// graph has no track under it.
func TestATallGraphIsGradedUpItsHeight(t *testing.T) {
	m := meterModel(graph.Braille)
	th := m.opts.Theme

	for _, ramp := range []string{"download", "upload"} {
		colors := m.graphRowColors(ramp, 8)
		if len(colors) != 8 {
			t.Fatalf("%s: %d colours for 8 rows", ramp, len(colors))
		}
		if want := th.Gradient(ramp, 1); colors[0] != want {
			t.Errorf("%s: the top row is %v, want the ramp's hot end %v", ramp, colors[0], want)
		}
		if want := th.Gradient(ramp, 0); colors[7] != want {
			t.Errorf("%s: the bottom row is %v, want the ramp's cold end %v", ramp, colors[7], want)
		}
		for i := 1; i < len(colors); i++ {
			if luminance(colors[i-1]) < luminance(colors[i]) {
				t.Errorf("%s: row %d is brighter than the row above it", ramp, i)
			}
		}
	}

	// One row is the dense view's sparkline, and it keeps the fixed point it
	// has always had: a single row has no height to grade along, and a
	// dense line that changed colour with this change would be a regression
	// dressed as a feature.
	if got, want := m.graphRowColors("download", 1), th.Gradient("download", 0.75); got[0] != want {
		t.Errorf("a one-row graph is %v, want the sparkline's own %v", got[0], want)
	}
}
