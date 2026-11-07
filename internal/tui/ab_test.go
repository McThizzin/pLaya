package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// abBodyAlt is guardBody with a different winner on the topic question; the
// numbers are still synthetic.
func abBodyAlt() string {
	b := strings.Replace(guardBody, `"choice":"security_testing"`, `"choice":"product_support"`, 1)
	b = strings.Replace(b, `"security_testing":0.85`, `"product_support":0.85`, 1)
	b = strings.Replace(b, `"product_support":0.02`, `"security_testing":0.02`, 1)
	return b
}

func byModel(alt string) func([]byte, int) (int, string) {
	return func(body []byte, _ int) (int, string) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		if req.Model == "english" {
			return 200, guardBody
		}
		return 200, alt
	}
}

func TestABCompareShowsDifferences(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(byModel(abBodyAlt()))

	m, _ = press(m, "A")
	if m.mode != screenAB || m.ab == nil {
		t.Fatal("A should open the A/B screen")
	}
	if m.ab.targets[0].Model != "auto" || m.ab.targets[1].Model != "english" {
		t.Fatalf("default targets = %q / %q, want auto / english",
			m.ab.targets[0].Model, m.ab.targets[1].Model)
	}

	m, cmd := press(m, "enter")
	if !m.ab.running {
		t.Fatal("enter should start both calls")
	}
	m = drain(t, m, cmd)
	if m.ab.running || !m.ab.ran {
		t.Fatalf("running=%v ran=%v", m.ab.running, m.ab.ran)
	}
	if m.ab.results[0].Resp == nil || m.ab.results[1].Resp == nil {
		t.Fatalf("results = %+v", m.ab.results)
	}
	if m.history == nil || m.history.Len() != 2 {
		t.Errorf("A/B should record both calls, history has %d", m.history.Len())
	}
	if got := m.predictor.Len(); got != 2 {
		t.Errorf("predictor samples = %d, want 2", got)
	}
	if !strings.Contains(m.ab.status, "differ on 1 of 5") {
		t.Errorf("status = %q", m.ab.status)
	}

	out := plain(m.View())
	for _, want := range []string{
		"A/B Compare", "A · auto → english", "B · english",
		"Differences", "topic", "product_support", "security_testing", "≠",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("A/B view missing %q:\n%s", want, out)
		}
	}
}

func TestABCompareUsesSecondServer(t *testing.T) {
	primary := newFake(t)
	secondary := newFake(t)
	m := connected(t, primary)
	primary.decideBody = abBodyAlt()
	secondary.decideBody = guardBody

	m, _ = press(m, "g")
	m, _ = press(m, "A")
	m, _ = press(m, "tab")
	if m.ab.selected != 1 {
		t.Fatalf("tab selected %d, want B", m.ab.selected)
	}
	m, _ = press(m, "u")
	if !m.ab.editing {
		t.Fatal("u should open the URL prompt")
	}
	m.ab.input.SetValue(secondary.URL)
	m, _ = press(m, "enter")
	if m.ab.editing {
		t.Fatal("enter should apply the URL")
	}
	if m.ab.targets[1].URL != secondary.URL {
		t.Fatalf("target B URL = %q, want %q", m.ab.targets[1].URL, secondary.URL)
	}

	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)
	if primary.decideCount() != 2 { // warmup + target A
		t.Errorf("primary calls = %d, want 2", primary.decideCount())
	}
	if secondary.decideCount() != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.decideCount())
	}
	out := plain(m.View())
	if !strings.Contains(out, "Differences") || !strings.Contains(out, "topic") {
		t.Errorf("cross-server view should show the disagreement:\n%s", out)
	}

	m, _ = press(m, "r")
	if m.ab.targets[1].URL != primary.URL {
		t.Errorf("r should reset B to the primary URL, got %q", m.ab.targets[1].URL)
	}
}

func TestABCancelIgnoresLateResults(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(byModel(abBodyAlt()))
	m, _ = press(m, "A")

	m, cmd := press(m, "enter")
	if !m.ab.running {
		t.Fatal("enter should start the run")
	}
	m, _ = press(m, "esc")
	if m.ab.running || m.ab.ran {
		t.Fatal("esc should cancel")
	}
	if !strings.Contains(m.ab.status, "cancelled") {
		t.Errorf("status = %q", m.ab.status)
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.ab.ran || m.ab.results[0].Resp != nil || m.ab.results[1].Resp != nil {
		t.Error("late results from a cancelled run must be ignored")
	}
}

func TestABOneTargetFailureStillShowsTheOther(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody

	dead := newFake(t)
	deadURL := dead.URL
	dead.Close()

	m, _ = press(m, "A")
	m.ab.targets[1].URL = deadURL
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)

	if m.ab.results[0].Resp == nil {
		t.Fatal("target A should have succeeded")
	}
	if m.ab.results[1].Err == nil {
		t.Fatal("target B should have failed")
	}
	if !strings.Contains(m.ab.status, "one target failed") {
		t.Errorf("status = %q", m.ab.status)
	}
	out := plain(m.View())
	if !strings.Contains(out, "A · auto") || !strings.Contains(out, "jailbreak") {
		t.Errorf("the successful side should still render:\n%s", out)
	}
	if got := m.predictor.Len(); got != 1 {
		t.Errorf("predictor samples = %d, want only the successful call", got)
	}
	if m.history == nil || m.history.Len() != 2 {
		t.Fatalf("history = %d entries, want 2", m.history.Len())
	}
	errs := 0
	for _, e := range m.history.List() {
		if e.Err != "" {
			errs++
		}
	}
	if errs != 1 {
		t.Errorf("history has %d failed entries, want 1", errs)
	}
}

func TestABViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.setDecideFunc(byModel(abBodyAlt()))
	m, _ = press(m, "A")
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)

	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: A/B view has %d lines:\n%s", size.w, size.h, len(lines), out)
		}
	}
}
