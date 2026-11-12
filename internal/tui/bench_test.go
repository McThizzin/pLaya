package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/bench"
)

func TestBenchRunAndCostReadout(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)

	// A normal run gets the new cost readout and teaches the predictor.
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)
	if m.result == nil || m.resultOf != "guard" {
		t.Fatal("guard result missing")
	}
	out := plain(m.View())
	for _, want := range []string{"Cost", "wall", "ms/q", "tok/s", "q/s", "predicted"} {
		if !strings.Contains(out, want) {
			t.Errorf("cost readout missing %q:\n%s", want, out)
		}
	}
	if m.predictor == nil || m.predictor.Len() != 1 {
		t.Fatalf("predictor samples = %v", m.predictor)
	}

	m, _ = press(m, "ctrl+b")
	if m.mode != screenBench || m.bench == nil {
		t.Fatal("ctrl+b should open the benchmark")
	}
	if m.bench.warmup != benchDefaultWarmup || m.bench.runs != benchDefaultRuns {
		t.Errorf("defaults = warmup %d runs %d", m.bench.warmup, m.bench.runs)
	}
	for i := 0; i < 3; i++ {
		m, _ = press(m, "[")
	}
	if m.bench.runs != benchMinRuns {
		t.Errorf("runs = %d, want %d", m.bench.runs, benchMinRuns)
	}
	m, _ = press(m, "w")
	if m.bench.warmup != 0 {
		t.Errorf("warmup = %d, want 0", m.bench.warmup)
	}

	before := f.decideCount()
	m, cmd = press(m, "enter")
	if !m.bench.running {
		t.Fatal("enter should start the benchmark")
	}
	m = drain(t, m, cmd)
	if m.bench.running {
		t.Fatal("benchmark should be finished")
	}
	if m.bench.done != benchMinRuns || len(m.bench.latencies) != benchMinRuns {
		t.Errorf("done = %d, latencies = %d", m.bench.done, len(m.bench.latencies))
	}
	if got := f.decideCount() - before; got != benchMinRuns {
		t.Errorf("decide calls = %d, want %d", got, benchMinRuns)
	}
	if len(m.bench.results) != 1 || m.bench.results[0].Stats.N != benchMinRuns {
		t.Fatalf("bench results = %+v", m.bench.results)
	}
	if m.benchStore == nil || m.benchStore.Len() != 1 {
		t.Errorf("bench store = %v", m.benchStore)
	}

	out = plain(m.View())
	for _, want := range []string{"Benchmark", "p50", "p95", "min", "max", "ms/q", "tok/s", "q/s", "latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("bench view missing %q:\n%s", want, out)
		}
	}
}

func TestBenchSavesTwoFamilies(t *testing.T) {
	f := newFake(t)
	path := filepath.Join(t.TempDir(), "bench.json")
	m := connectedWith(t, f, Options{BenchPath: path})
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)

	m, _ = press(m, "ctrl+b")
	for i := 0; i < 3; i++ {
		m, _ = press(m, "[")
	}
	m, _ = press(m, "w")
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)

	m, _ = press(m, "f")
	if m.bench.model != "english" {
		t.Fatalf("family after f = %q, want english", m.bench.model)
	}
	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)

	if len(m.bench.results) != 2 {
		t.Fatalf("results = %d, want 2", len(m.bench.results))
	}
	if m.bench.results[0].Model != "english" || m.bench.results[1].Model != "" {
		t.Errorf("result families = %q, %q", m.bench.results[0].Model, m.bench.results[1].Model)
	}
	out := plain(m.View())
	for _, want := range []string{"auto", "english", "latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("side-by-side view missing %q:\n%s", want, out)
		}
	}

	loaded, err := bench.Load(path, bench.DefaultMax)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Len() != 2 {
		t.Errorf("saved results = %d, want 2", loaded.Len())
	}
}

func TestBenchCancelThenResume(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)
	m, _ = press(m, "ctrl+b")
	for i := 0; i < 3; i++ {
		m, _ = press(m, "[")
	}
	m, _ = press(m, "w")

	m, cmd := press(m, "enter")
	if !m.bench.running {
		t.Fatal("bankmark should be running")
	}
	m, _ = press(m, "esc")
	if m.bench.running {
		t.Fatal("esc should cancel")
	}
	m = drain(t, m, cmd) // the late result belongs to the cancelled run
	if m.bench.done != 0 || len(m.bench.latencies) != 0 {
		t.Errorf("cancelled run kept %d/%d results", m.bench.done, len(m.bench.latencies))
	}
	if !strings.Contains(m.bench.status, "cancelled") {
		t.Errorf("status = %q", m.bench.status)
	}

	m, cmd = press(m, "enter")
	m = drain(t, m, cmd)
	if m.bench.done != benchMinRuns {
		t.Errorf("resumed done = %d, want %d", m.bench.done, benchMinRuns)
	}
}

func TestBenchViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(answerAll)
	m, _ = press(m, "ctrl+b")
	for i := 0; i < 3; i++ {
		m, _ = press(m, "[")
	}
	m, _ = press(m, "w")
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)

	for _, size := range []struct{ w, h int }{{100, 30}, {120, 40}, {140, 50}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: bench view has %d lines:\n%s", size.w, size.h, len(lines), out)
		}
	}
}
