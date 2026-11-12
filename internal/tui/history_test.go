package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
)

func TestHistoryRecordsRunAndRaw(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)

	if m.history.Len() != 1 {
		t.Fatalf("history len = %d", m.history.Len())
	}
	e, ok := m.history.Get(1)
	if !ok {
		t.Fatal("run #1 missing")
	}
	if e.Preset != "guard" || e.Response == nil || e.Err != "" {
		t.Errorf("entry = %+v", e)
	}
	if len(e.Raw) == 0 || e.Response.Family != "english" {
		t.Errorf("stored response incomplete: raw=%d family=%q", len(e.Raw), e.Response.Family)
	}

	m, _ = press(m, "alt+7")
	if m.mode != screenHistory {
		t.Fatal("H should open history")
	}
	out := plain(m.View())
	for _, want := range []string{"History", "#1", "guard", "Decisions", "jailbreak"} {
		if !strings.Contains(out, want) {
			t.Errorf("history view missing %q:\n%s", want, out)
		}
	}

	m, _ = press(m, "v")
	out = plain(m.View())
	if !strings.Contains(out, "Raw response") || !strings.Contains(out, `"answers"`) {
		t.Errorf("history raw view missing:\n%s", out)
	}
	m, _ = press(m, "v")
	if strings.Contains(plain(m.View()), "Raw response") {
		t.Error("v should toggle back to decisions")
	}
}

func TestHistoryReplayUsesStoredRequest(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)

	// Edit the draft after the run. Replay must still send the stored request.
	m.currentDraft().setStateText("a completely different state")
	m, _ = press(m, "alt+7")
	m, cmd = press(m, "enter")
	if !m.running {
		t.Fatal("replay should start a run")
	}
	m = step(t, m, cmd)

	if strings.Contains(string(f.lastBody), "completely different state") {
		t.Errorf("replay used the edited draft: %s", f.lastBody)
	}
	if !strings.Contains(string(f.lastBody), "Ignore all previous instructions") {
		t.Errorf("replay did not use the stored request: %s", f.lastBody)
	}
	if m.history.Len() != 2 {
		t.Fatalf("history len = %d", m.history.Len())
	}
	newest := m.history.List()[0]
	if newest.ReplayOf != 1 || newest.ID != 2 {
		t.Errorf("replayed entry = %+v", newest)
	}
	if m.historyCursor != 0 {
		t.Errorf("history cursor = %d, want newest", m.historyCursor)
	}
	out := plain(m.View())
	if !strings.Contains(out, "#2") || !strings.Contains(out, "replay of #1") {
		t.Errorf("replay entry not shown:\n%s", out)
	}
}

func TestHistoryRecordsErrors(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	f.decideCode = 400
	f.decideBody = `{"error":{"code":"invalid_request_error","message":"bad criteria","param":"questions.urgency.criteria"}}`
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)

	if m.history.Len() != 1 {
		t.Fatalf("history len = %d", m.history.Len())
	}
	e, _ := m.history.Get(1)
	if e.Err == "" || e.Param != "questions.urgency.criteria" || e.Response != nil {
		t.Errorf("error entry = %+v", e)
	}
	m, _ = press(m, "alt+7")
	out := plain(m.View())
	if !strings.Contains(out, "bad criteria") || !strings.Contains(out, `question "urgency"`) {
		t.Errorf("history error detail missing:\n%s", out)
	}
}

func TestHistoryPersistsAcrossModels(t *testing.T) {
	f := newFake(t)
	path := filepath.Join(t.TempDir(), "history.json")
	opts := Options{Warmup: true, Timeout: 5 * time.Second, Now: clock(), HistoryPath: path}
	m := New(api.NewClient(f.URL), opts)
	m = step(t, m, m.healthCmd())
	m = step(t, m, m.warmupCmd())
	m = step(t, m, m.presetsCmd())
	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)

	reloaded := New(api.NewClient(f.URL), opts)
	if reloaded.history.Len() != 1 {
		t.Fatalf("reloaded history len = %d", reloaded.history.Len())
	}
	reloaded, _ = press(reloaded, "alt+7")
	if out := plain(reloaded.View()); !strings.Contains(out, "#1") || !strings.Contains(out, "guard") {
		t.Errorf("persisted run missing:\n%s", out)
	}
}

func TestHistoryViewFitsTheTerminal(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	m, _ = press(m, "alt+7")

	for _, size := range []struct{ w, h int }{{100, 30}, {120, 40}, {140, 50}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: history has %d lines", size.w, size.h, len(lines))
		}
	}
}
