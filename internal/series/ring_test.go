package series

import (
	"math"
	"reflect"
	"testing"
)

func TestRingKeepsTheMostRecentSamples(t *testing.T) {
	r := New(3)
	if got := r.Window(3); len(got) != 0 {
		t.Fatalf("a fresh ring should be empty, got %v", got)
	}

	r.Push(1)
	r.Push(2)
	if got := r.Window(3); !reflect.DeepEqual(got, []float64{1, 2}) {
		t.Errorf("got %v, want [1 2]", got)
	}

	// Overflowing must drop the oldest, not the newest: a graph scrolls left.
	r.Push(3)
	r.Push(4)
	if got := r.Window(3); !reflect.DeepEqual(got, []float64{2, 3, 4}) {
		t.Errorf("got %v, want [2 3 4]", got)
	}
	if r.Len() != 3 {
		t.Errorf("Len = %d, want 3", r.Len())
	}
}

func TestWindowIsOldestFirst(t *testing.T) {
	// Order matters: the renderer draws left to right and the newest sample
	// belongs on the right, as it does in btop.
	r := New(8)
	for i := 1; i <= 5; i++ {
		r.Push(float64(i))
	}
	if got := r.Window(3); !reflect.DeepEqual(got, []float64{3, 4, 5}) {
		t.Errorf("got %v, want [3 4 5]", got)
	}
	if got := r.Window(99); !reflect.DeepEqual(got, []float64{1, 2, 3, 4, 5}) {
		t.Errorf("asking for more than is stored should return everything, got %v", got)
	}
}

func TestMaxOverWindow(t *testing.T) {
	r := New(5)
	for _, v := range []float64{10, 50, 20, 5, 1} {
		r.Push(v)
	}
	// The peak must be taken over the window actually drawn, not the whole
	// buffer, or the graph stays flat long after a burst has scrolled away.
	if got := r.Max(2); got != 5 {
		t.Errorf("Max(2) = %v, want 5", got)
	}
	if got := r.Max(5); got != 50 {
		t.Errorf("Max(5) = %v, want 50", got)
	}
	if got := New(4).Max(4); got != 0 {
		t.Errorf("Max of an empty ring = %v, want 0", got)
	}
}

func TestResizePreservesTheTail(t *testing.T) {
	r := New(4)
	for i := 1; i <= 4; i++ {
		r.Push(float64(i))
	}

	// Growing keeps everything: the terminal got wider, so more history fits.
	r.Resize(6)
	if got := r.Window(6); !reflect.DeepEqual(got, []float64{1, 2, 3, 4}) {
		t.Errorf("after growing, got %v, want [1 2 3 4]", got)
	}
	r.Push(5)
	r.Push(6)
	r.Push(7)
	if got := r.Window(6); !reflect.DeepEqual(got, []float64{2, 3, 4, 5, 6, 7}) {
		t.Errorf("got %v, want [2 3 4 5 6 7]", got)
	}

	// Shrinking keeps the newest, because that is the part still on screen.
	r.Resize(2)
	if got := r.Window(2); !reflect.DeepEqual(got, []float64{6, 7}) {
		t.Errorf("after shrinking, got %v, want [6 7]", got)
	}

	// Resizing to the current capacity must not disturb anything.
	r.Resize(2)
	if got := r.Window(2); !reflect.DeepEqual(got, []float64{6, 7}) {
		t.Errorf("a no-op resize changed the contents: %v", got)
	}
}

func TestDegenerateCapacities(t *testing.T) {
	// A zero or negative capacity would otherwise divide by zero on the first
	// push. The ring clamps instead, because the caller derives capacity from
	// a terminal width that can legitimately be reported as zero.
	for _, capacity := range []int{0, -1} {
		r := New(capacity)
		r.Push(1)
		r.Push(2)
		if r.Len() != 1 {
			t.Errorf("New(%d): Len = %d, want 1", capacity, r.Len())
		}
		if got := r.Window(1); !reflect.DeepEqual(got, []float64{2}) {
			t.Errorf("New(%d): got %v, want [2]", capacity, got)
		}
	}
}

func TestNonFiniteSamplesAreIgnored(t *testing.T) {
	// A rate computed from a zero elapsed time would arrive as NaN or Inf and
	// poison every subsequent Max, flattening the graph permanently.
	r := New(4)
	r.Push(10)
	r.Push(math.NaN())
	r.Push(math.Inf(1))
	r.Push(20)

	if got := r.Window(4); !reflect.DeepEqual(got, []float64{10, 20}) {
		t.Errorf("got %v, want [10 20]", got)
	}
	if got := r.Max(4); got != 20 {
		t.Errorf("Max = %v, want 20", got)
	}
}
