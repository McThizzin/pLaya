package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
	"playa/internal/history"
	"playa/internal/window"
)

func TestWindowedRunSequentialAndParams(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue(strings.Repeat("A long document sentence. ", 200))

	m, _ = press(m, "ctrl+w")
	if m.mode != screenWindow || m.windowed == nil {
		t.Fatalf("ctrl+w should open windowed mode (mode=%v)", m.mode)
	}
	e := m.windowed
	if e.size != window.DefaultSize || len(e.windows) < 2 {
		t.Fatalf("size=%d windows=%d", e.size, len(e.windows))
	}
	for _, w := range e.windows {
		if w.Tokens > e.size {
			t.Errorf("window %d has %d tokens, size %d", w.Index, w.Tokens, e.size)
		}
	}
	original := len(e.windows)

	m, _ = press(m, "]")
	if e.size != window.DefaultSize+windowSizeStep {
		t.Errorf("size after ] = %d", e.size)
	}
	if len(e.windows) >= original {
		t.Errorf("a larger window should not need more windows: %d -> %d", original, len(e.windows))
	}
	m, _ = press(m, "[")
	if e.size != window.DefaultSize {
		t.Errorf("size after [ = %d", e.size)
	}

	before := e.overlapPct
	m, _ = press(m, "o")
	if e.overlapPct == before {
		t.Errorf("o should cycle overlap away from %v", before)
	}
	if e.strongest != -1 {
		t.Errorf("changing parameters should clear the strongest marker, got %d", e.strongest)
	}

	f.setDecideFunc(answerAll)
	baseCalls := f.decideCount()
	m, cmd := press(m, "enter")
	if !e.running {
		t.Fatal("enter should start the run")
	}
	m = drain(t, m, cmd)
	if e.running {
		t.Errorf("run did not finish: %q", e.status)
	}
	if got := f.decideCount() - baseCalls; got != len(e.windows) {
		t.Errorf("decide calls = %d, want %d", got, len(e.windows))
	}
	wantHistory := min(len(e.windows), history.DefaultMax)
	if got := len(m.history.List()); got != wantHistory {
		t.Errorf("history entries = %d, want %d", got, wantHistory)
	}
	if e.strongest < 0 {
		t.Errorf("expected a strongest window, status %q", e.status)
	}
	if !strings.Contains(e.status, "strongest") {
		t.Errorf("status = %q", e.status)
	}

	out := plain(m.View())
	for _, want := range []string{"Windowed Run", "Windows", "strongest", "mean winner probability"} {
		if !strings.Contains(out, want) {
			t.Errorf("window view missing %q:\n%s", want, out)
		}
	}

	m, _ = press(m, "v")
	if !e.raw {
		t.Fatal("v should show the raw response")
	}
	if out := plain(m.View()); !strings.Contains(out, `"answers"`) {
		t.Errorf("raw view missing the response:\n%s", out)
	}

	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 12}, {50, 8}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		text := plain(next.(Model).View())
		if lines := strings.Split(text, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: window view has %d lines:\n%s", size.w, size.h, len(lines), text)
		}
	}
}

func TestWindowNeedsStateText(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue("   ")

	m, _ = press(m, "ctrl+w")
	if m.mode != screenState {
		t.Errorf("empty text should stay on the canvas, mode=%v", m.mode)
	}
	if m.stateEd.err == "" {
		t.Error("empty text should explain why windowed mode did not open")
	}
}

func TestWindowCancelIgnoresInFlight(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue(strings.Repeat("cancel this run ", 100))
	m, _ = press(m, "ctrl+w")
	f.setDecideFunc(answerAll)

	m, cmd := press(m, "enter")
	if cmd == nil || !m.windowed.running {
		t.Fatal("enter should start a run")
	}
	m, _ = press(m, "esc")
	if m.windowed.running || !strings.Contains(m.windowed.status, "cancelled") {
		t.Fatalf("cancel: running=%v status=%q", m.windowed.running, m.windowed.status)
	}
	before := len(m.history.List())
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.windowed.results[0].Resp != nil || m.windowed.results[0].Err != nil {
		t.Error("a cancelled window result must be ignored")
	}
	if got := len(m.history.List()); got != before {
		t.Errorf("cancelled result was recorded in history (%d -> %d)", before, got)
	}

	m, _ = press(m, "esc")
	if m.mode != screenState {
		t.Errorf("esc after cancel should return to the State Canvas, mode=%v", m.mode)
	}
}

func TestStrongestWindowPicksHighestMean(t *testing.T) {
	qs := api.QuestionSet{
		{Name: "a", Question: api.Question{Type: api.TypeNoul}},
		{Name: "b", Question: api.Question{Type: api.TypeNoul}},
	}
	resp := func(a, b float64) *api.DecideResponse {
		return &api.DecideResponse{Answers: map[string]api.Answer{
			"a": {Type: api.TypeNoul, Noul: &a},
			"b": {Type: api.TypeNoul, Noul: &b},
		}}
	}
	results := []windowResult{
		{Resp: resp(0.6, 0.6)},
		{Resp: resp(0.9, 0.9)},
		{Err: errors.New("failed window")},
		{Resp: resp(0.7, 0.7)},
	}
	if got := strongestWindow(results, qs); got != 1 {
		t.Errorf("strongest = %d, want 1", got)
	}
	if got := strongestWindow(nil, qs); got != -1 {
		t.Errorf("empty results = %d, want -1", got)
	}

	// A tie keeps the first window so the marker is stable.
	tie := []windowResult{{Resp: resp(0.8, 0.8)}, {Resp: resp(0.8, 0.8)}}
	if got := strongestWindow(tie, qs); got != 0 {
		t.Errorf("tie = %d, want 0", got)
	}
}
