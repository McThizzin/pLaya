package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/snippet"
)

func snippetHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestSnippetsSaveListAndDelete(t *testing.T) {
	snippetHome(t)
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	d := m.currentDraft()
	d.state[d.stateKey] = "a saved prompt about a refund"
	d.state["ticket"] = "T-42"

	m, _ = press(m, "S")
	if m.mode != screenSnippets || m.snippets == nil {
		t.Fatalf("S should open the snippet library (mode=%v)", m.mode)
	}
	m, _ = press(m, "s")
	if !m.snippets.prompt {
		t.Fatal("s should open the save prompt")
	}
	m.snippets.input.SetValue("Support / Refund")
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)
	if m.snippets.err != "" {
		t.Fatalf("save: %s", m.snippets.err)
	}
	if len(m.snippets.names) != 1 || m.snippets.names[0] != "support_refund" {
		t.Fatalf("names = %v", m.snippets.names)
	}

	saved, err := snippet.Load("support_refund")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Preset != "guard" || saved.StateKey != d.stateKey {
		t.Errorf("saved = %+v", saved)
	}
	if saved.State[d.stateKey] != "a saved prompt about a refund" || saved.State["ticket"] != "T-42" {
		t.Errorf("saved state = %#v", saved.State)
	}

	out := plain(m.View())
	for _, want := range []string{"Snippets", "support_refund", "a saved prompt about a refund", "T-42"} {
		if !strings.Contains(out, want) {
			t.Errorf("snippet view missing %q:\n%s", want, out)
		}
	}

	m, _ = press(m, "d")
	if m.snippets.pendingDelete != "support_refund" {
		t.Fatalf("pendingDelete = %q", m.snippets.pendingDelete)
	}
	m, _ = press(m, "n")
	if m.snippets.pendingDelete != "" {
		t.Error("an unrelated key should cancel the delete")
	}
	m, _ = press(m, "d")
	m, _ = press(m, "y")
	if m.snippets.err != "" {
		t.Fatalf("delete: %s", m.snippets.err)
	}
	if len(m.snippets.names) != 0 {
		t.Errorf("names after delete = %v", m.snippets.names)
	}
	if _, err := snippet.Load("support_refund"); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
	}
	if !strings.Contains(plain(m.View()), "No snippets yet") {
		t.Errorf("empty view missing hint:\n%s", plain(m.View()))
	}
}

func TestSnippetLoadFromStateReplacesStateAndKey(t *testing.T) {
	snippetHome(t)
	if _, err := snippet.Save("other", snippet.Snippet{
		Preset:   "email",
		StateKey: "document",
		State: map[string]any{
			"document": "loaded body text",
			"meta":     map[string]any{"x": true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	f := newFake(t)
	m := openState(t, f)
	d := m.currentDraft()
	oldKey := d.stateKey
	if oldKey == "document" {
		t.Fatal("test needs a different state key")
	}

	m, _ = press(m, "ctrl+l")
	if m.mode != screenSnippets || m.snippets.returnTo != screenState {
		t.Fatalf("ctrl+l mode=%v return=%v", m.mode, m.snippets.returnTo)
	}
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)
	if m.mode != screenSnippets {
		t.Errorf("loading should stay in the library, mode=%v", m.mode)
	}
	if d.stateKey != "document" || d.stateText() != "loaded body text" {
		t.Errorf("draft key=%q text=%q", d.stateKey, d.stateText())
	}
	if _, ok := d.state[oldKey]; ok {
		t.Errorf("old key %q survived the load", oldKey)
	}
	if !d.dirty {
		t.Error("a loaded snippet should mark the draft dirty")
	}
	if m.stateEd.area.Value() != "loaded body text" {
		t.Errorf("state editor textarea = %q", m.stateEd.area.Value())
	}
	req := d.request("")
	state, _ := req.State.(map[string]any)
	if state["document"] != "loaded body text" {
		t.Errorf("request state = %#v", req.State)
	}

	m, _ = press(m, "esc")
	if m.mode != screenState {
		t.Errorf("esc should return to the State Canvas, mode=%v", m.mode)
	}
	out := plain(m.View())
	if !strings.Contains(out, "loaded body text") {
		t.Errorf("state view did not refresh:\n%s", out)
	}
}

func TestSnippetSaveUsesTheLatestCanvasText(t *testing.T) {
	snippetHome(t)
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue("typed in the canvas")

	m, _ = press(m, "ctrl+l")
	m, _ = press(m, "s")
	m.snippets.input.SetValue("canvas")
	m, cmd := press(m, "enter")
	m = drain(t, m, cmd)
	if m.snippets.err != "" {
		t.Fatalf("save: %s", m.snippets.err)
	}
	saved, err := snippet.Load("canvas")
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.State[saved.StateKey]; got != "typed in the canvas" {
		t.Errorf("saved text = %v", got)
	}
}

func TestSnippetPromptCancelAndViewFits(t *testing.T) {
	snippetHome(t)
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "S")
	m, _ = press(m, "s")

	// Long values exercise truncation in the prompt and detail panes.
	m.snippets.input.SetValue(strings.Repeat("long-name ", 20))
	for _, size := range []struct{ w, h int }{{80, 24}, {60, 12}, {50, 8}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		out := plain(next.(Model).View())
		if lines := strings.Split(out, "\n"); len(lines) > size.h {
			t.Errorf("%dx%d: snippet view has %d lines:\n%s", size.w, size.h, len(lines), out)
		}
	}

	m, _ = press(m, "esc")
	if m.snippets.prompt {
		t.Error("esc should close the save prompt")
	}
	m, _ = press(m, "esc")
	if m.mode != screenBrowse {
		t.Errorf("mode = %v, want browse", m.mode)
	}
}
