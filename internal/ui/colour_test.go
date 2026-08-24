package ui

import (
	"testing"

	"github.com/Seykhel/rclonetop/internal/theme"
)

// luminance is Rec. 709 relative brightness, which is what decides whether text
// can be read against a dark background. Plain channel averages would call
// #4f43a3 and #43a34f equally legible, and the eye does not.
func luminance(c theme.Color) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

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

func TestInactiveIsReservedForWhatIsInert(t *testing.T) {
	// inactive_fg is #40 in the built-in theme -- dark grey, chosen to mean
	// "switched off". It used to label pid, up, rss, thr, rd and wr, which made
	// two thirds of the screen nearly invisible and left nothing to say
	// "switched off" with. Labels are main_fg now, and the hierarchy between a
	// label and its value is carried by weight, which survives a terminal with
	// eight colours where a distinction made of colour would not.
	th := theme.Default()
	m := New(nil, Options{Theme: th}, nil)

	if luminance(th.Color("inactive_fg")) >= luminance(th.Color("main_fg"))/2 {
		t.Skip("this theme's inactive_fg is not dark enough for the rule to matter")
	}
	if got, want := m.label().GetForeground(), th.Color("main_fg").Lipgloss(); got != want {
		t.Errorf("label foreground = %v, want main_fg %v", got, want)
	}
	if !m.value().GetBold() {
		t.Error("a value is not bold, so nothing distinguishes it from its own label")
	}
}
