// Package telemetry keeps a rolling median of server latency per request kind.
//
// The first call after startup or an idle pause was consistently several times
// slower than steady state (250-650 ms vs 60 ms for a single noul), so those
// "cold" calls are reported but kept out of the median.
package telemetry

import (
	"sort"
	"time"
)

// Tracker records latency samples. It is not safe for concurrent use; the TUI
// updates it from the single Bubble Tea update loop.
type Tracker struct {
	window    int
	coldAfter time.Duration
	samples   map[string][]float64
	last      time.Time
	seen      bool
}

// New returns a tracker that keeps the last window samples per kind and treats
// a call as cold when more than coldAfter has passed since the previous call.
func New(window int, coldAfter time.Duration) *Tracker {
	if window < 1 {
		window = 10
	}
	return &Tracker{window: window, coldAfter: coldAfter, samples: map[string][]float64{}}
}

// Observe records one call and reports whether it was cold. Cold calls update
// the idle clock but are not added to the samples.
func (t *Tracker) Observe(kind string, latencyMs float64, at time.Time) (cold bool) {
	cold = !t.seen || at.Sub(t.last) > t.coldAfter
	t.seen = true
	t.last = at
	if cold {
		return true
	}
	s := append(t.samples[kind], latencyMs)
	if len(s) > t.window {
		s = s[len(s)-t.window:]
	}
	t.samples[kind] = s
	return false
}

// Median returns the median of the recorded samples for a kind, and how many
// there are. ok is false when there are none yet.
func (t *Tracker) Median(kind string) (ms float64, n int, ok bool) {
	s := t.samples[kind]
	if len(s) == 0 {
		return 0, 0, false
	}
	c := append([]float64(nil), s...)
	sort.Float64s(c)
	mid := len(c) / 2
	if len(c)%2 == 1 {
		return c[mid], len(c), true
	}
	return (c[mid-1] + c[mid]) / 2, len(c), true
}
