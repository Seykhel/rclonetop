// Package series keeps the recent history a graph is drawn from.
//
// A graph is only ever as wide as the terminal, so the history is bounded by
// that width rather than by time. Keeping more would be memory spent on samples
// that can never be drawn.
package series

import "math"

// Ring is a fixed-capacity buffer of samples, oldest dropped first.
//
// The zero value is not usable; call New.
type Ring struct {
	data []float64
	// next is where the following sample will be written.
	next int
	// n is how many slots are populated, saturating at capacity.
	n int
}

// New returns a ring holding at most capacity samples. A capacity below one is
// raised to one: callers derive it from a terminal width, which can legitimately
// be reported as zero before the first resize message arrives.
func New(capacity int) *Ring {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring{data: make([]float64, capacity)}
}

// Cap reports the ring's capacity.
func (r *Ring) Cap() int { return len(r.data) }

// Len reports how many samples are stored.
func (r *Ring) Len() int { return r.n }

// Push appends a sample, discarding the oldest when the ring is full.
//
// Values that are not finite are dropped. A rate divided by a zero elapsed time
// arrives as NaN or infinity, and a single one of those would poison every
// later Max and flatten the graph for good.
func (r *Ring) Push(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	r.data[r.next] = v
	r.next = (r.next + 1) % len(r.data)
	if r.n < len(r.data) {
		r.n++
	}
}

// Window returns the most recent n samples, oldest first.
//
// The order matters: the renderer draws left to right with the newest sample on
// the right, so the graph scrolls the way btop's does. Fewer than n samples are
// returned when fewer are stored; the caller pads or right-aligns as it sees
// fit.
func (r *Ring) Window(n int) []float64 {
	if n > r.n {
		n = r.n
	}
	if n <= 0 {
		return nil
	}

	out := make([]float64, n)
	// next points one past the newest sample, so the window starts n slots
	// behind it.
	start := (r.next - n + len(r.data)) % len(r.data)
	for i := 0; i < n; i++ {
		out[i] = r.data[(start+i)%len(r.data)]
	}
	return out
}

// Max returns the largest value among the most recent n samples.
//
// It is deliberately scoped to the window rather than the whole buffer: scaling
// a graph to a peak that has already scrolled off screen leaves it flat and
// unreadable long after the burst is over.
func (r *Ring) Max(n int) float64 {
	max := 0.0
	for _, v := range r.Window(n) {
		if v > max {
			max = v
		}
	}
	return max
}

// Resize changes the capacity, keeping the newest samples that still fit.
//
// The newest are kept because they are the ones still on screen: a terminal
// that narrows should lose the left edge of its history, not the right.
func (r *Ring) Resize(capacity int) {
	if capacity < 1 {
		capacity = 1
	}
	if capacity == len(r.data) {
		return
	}

	keep := r.Window(capacity)
	data := make([]float64, capacity)
	copy(data, keep)

	r.data = data
	r.n = len(keep)
	r.next = len(keep) % capacity
}
