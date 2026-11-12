package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/langdetect"
)

func TestRoutePreviewFeedsLengthGuard(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m.opts.Detect = func(ctx context.Context, text string) (langdetect.Preview, error) {
		return langdetect.Preview{
			Script: "latin", Words: 5, Route: "multilingual",
			Reason: "latin but weak english 0/5",
		}, nil
	}
	m, _ = press(m, "g")
	m, _ = press(m, "ctrl+e")

	// Skip the debounce timer and run the detector command directly.
	next, cmd := m.Update(routeTickMsg{seq: m.routeSeq, text: m.routeText})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("route tick should start a detect command")
	}
	m = step(t, m, cmd)

	if m.routePending || m.routeErr != "" {
		t.Fatalf("pending=%v err=%q", m.routePending, m.routeErr)
	}
	out := plain(m.View())
	for _, want := range []string{"route: multilingual", "latin but weak english 0/5", "multilingual"} {
		if !strings.Contains(out, want) {
			t.Errorf("state view missing %q:\n%s", want, out)
		}
	}
	if got := m.previewFamily(m.currentDraft()); got != "multilingual" {
		t.Errorf("previewFamily = %q, want multilingual", got)
	}
}

func TestStaleRouteResultIgnored(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	m, _ = press(m, "ctrl+e")
	seq := m.routeSeq
	m.routePending = true

	next, _ := m.Update(routeMsg{seq: seq - 1, preview: langdetect.Preview{Route: "english"}})
	m = next.(Model)
	if m.routePreview.Route != "" || !m.routePending {
		t.Fatalf("stale result was applied: %+v pending=%v", m.routePreview, m.routePending)
	}
}

func TestCancelInFlightRequest(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	f.decideBody = guardBody

	m, cmd := press(m, "enter")
	if !m.running || m.runSeq == 0 {
		t.Fatal("request should be running")
	}
	seq := m.runSeq

	m, _ = press(m, "esc")
	if m.running {
		t.Fatal("esc should clear running")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[len(m.toasts)-1].text, "cancelled") {
		t.Errorf("toasts = %+v, want a cancellation message", m.toasts)
	}

	// The command may already have a response; its stale sequence must not
	// replace the UI.
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.result != nil || m.runErr != "" {
		t.Errorf("stale response applied: result=%v err=%q", m.result, m.runErr)
	}
	if m.runSeq == seq {
		t.Error("run sequence should advance on cancel")
	}
}

func TestServerErrorParamMapsToQuestion(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	f.decideCode = 400
	f.decideBody = `{"error":{"code":"invalid_request_error","message":"criteria must be an object","param":"questions.urgency.criteria"}}`
	m, _ = press(m, "e")

	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	if m.runErr == "" || m.runErrParam != "questions.urgency.criteria" {
		t.Fatalf("err=%q param=%q", m.runErr, m.runErrParam)
	}
	if !strings.Contains(plain(m.View()), `question "urgency"`) {
		t.Errorf("detail missing param hint:\n%s", plain(m.View()))
	}

	m, _ = press(m, "alt+2")
	if m.mode != screenForge || m.forgeEd == nil {
		t.Fatal("forge did not open")
	}
	if got := m.currentDraft().questions[m.forgeEd.cursor].Name; got != "urgency" {
		t.Errorf("cursor question = %q, want urgency", got)
	}
}
