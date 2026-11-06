package telemetry

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func TestFirstCallIsCold(t *testing.T) {
	tr := New(10, 10*time.Second)
	if !tr.Observe("preset", 652, t0) {
		t.Error("first call should be cold")
	}
	if _, _, ok := tr.Median("preset"); ok {
		t.Error("a cold call must not become a sample")
	}
}

func TestSteadyCallsAreSampled(t *testing.T) {
	tr := New(10, 10*time.Second)
	tr.Observe("warmup", 650, t0) // cold, sets the idle clock
	for i, ms := range []float64{61, 58, 64} {
		if tr.Observe("single", ms, t0.Add(time.Duration(i+1)*time.Second)) {
			t.Errorf("call %d should not be cold", i)
		}
	}
	m, n, ok := tr.Median("single")
	if !ok || n != 3 || m != 61 {
		t.Errorf("median = %v n=%d ok=%v", m, n, ok)
	}
}

func TestIdlePauseMakesNextCallCold(t *testing.T) {
	tr := New(10, 10*time.Second)
	tr.Observe("warmup", 650, t0)
	tr.Observe("single", 60, t0.Add(time.Second))
	if !tr.Observe("single", 270, t0.Add(40*time.Second)) {
		t.Error("a call after a 39 s pause should be cold")
	}
	if m, n, _ := tr.Median("single"); n != 1 || m != 60 {
		t.Errorf("cold call leaked into samples: median %v n=%d", m, n)
	}
	// and the one right after is steady again
	if tr.Observe("single", 62, t0.Add(41*time.Second)) {
		t.Error("follow-up call should not be cold")
	}
}

func TestEvenCountAveragesMiddle(t *testing.T) {
	tr := New(10, time.Hour)
	tr.Observe("w", 1, t0)
	for i, ms := range []float64{10, 20, 30, 40} {
		tr.Observe("k", ms, t0.Add(time.Duration(i+1)*time.Second))
	}
	if m, _, _ := tr.Median("k"); m != 25 {
		t.Errorf("median = %v, want 25", m)
	}
}

func TestWindowKeepsLastSamples(t *testing.T) {
	tr := New(3, time.Hour)
	tr.Observe("w", 1, t0)
	for i := 1; i <= 6; i++ {
		tr.Observe("k", float64(i*10), t0.Add(time.Duration(i)*time.Second))
	}
	m, n, _ := tr.Median("k") // last three: 40, 50, 60
	if n != 3 || m != 50 {
		t.Errorf("median = %v n=%d, want 50 over 3", m, n)
	}
}

func TestKindsAreSeparate(t *testing.T) {
	tr := New(10, time.Hour)
	tr.Observe("w", 1, t0)
	tr.Observe("single", 60, t0.Add(time.Second))
	tr.Observe("preset", 359, t0.Add(2*time.Second))
	a, _, _ := tr.Median("single")
	b, _, _ := tr.Median("preset")
	if a != 60 || b != 359 {
		t.Errorf("single=%v preset=%v", a, b)
	}
}
