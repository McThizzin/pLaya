package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/snippet"
)

func TestBareTabKeySwitchesFromBrowse(t *testing.T) {
	m := connected(t, newFake(t))
	m, _ = press(m, "7")
	if m.mode != screenHistory {
		t.Fatalf("7 should open History, mode=%v", m.mode)
	}
	m, _ = press(m, "0")
	if m.mode != screenBrowse {
		t.Fatalf("0 should return home, mode=%v", m.mode)
	}
}

func TestAltTabKeyWorksWhileTyping(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m, _ = press(m, "alt+3")
	if m.mode != screenBatch || m.batch == nil {
		t.Fatalf("alt+3 should open Batch while the canvas has text focus, mode=%v", m.mode)
	}
}

func TestBareDigitIsTypedWhileEditing(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	before := m.stateEd.area.Value()
	m, _ = press(m, "3")
	if m.mode != screenState {
		t.Fatalf("a bare digit must not switch tabs while editing, mode=%v", m.mode)
	}
	if m.stateEd.area.Value() != before+"3" {
		t.Errorf("digit was not typed into the canvas: %q", m.stateEd.area.Value())
	}
}

func TestTabCyclesOnlyWhereUnbound(t *testing.T) {
	m := connected(t, newFake(t))
	m, _ = press(m, "tab")
	if m.mode != screenState {
		t.Fatalf("tab from pLaya should open State, mode=%v", m.mode)
	}
	m, _ = press(m, "tab")
	if m.mode != screenState {
		t.Errorf("State binds tab for its fields; mode moved to %v", m.mode)
	}
	if m.stateEd.sub != stateSubFields {
		t.Errorf("State tab should switch to fields, sub=%v", m.stateEd.sub)
	}
}

func TestTabStateIsPersistent(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue("persist this text")
	m, _ = press(m, "ctrl+s")
	if m.mode != screenBrowse {
		t.Fatalf("ctrl+s should apply and return home, mode=%v", m.mode)
	}
	m, _ = press(m, "alt+1")
	if m.mode != screenState || m.stateEd == nil {
		t.Fatalf("alt+1 should reuse the State tab, mode=%v", m.mode)
	}
	if got := m.stateEd.area.Value(); got != "persist this text" {
		t.Errorf("state text = %q, want the persisted edit", got)
	}
}

func TestEscapePausesThenLeavesState(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	if !m.stateEd.focused {
		t.Fatal("the canvas should start focused")
	}
	m, _ = press(m, "esc")
	if m.mode != screenState || m.stateEd.focused {
		t.Fatalf("first esc should pause text focus: mode=%v focused=%v", m.mode, m.stateEd.focused)
	}
	m, _ = press(m, "esc")
	if m.mode != screenBrowse {
		t.Fatalf("second esc should return home, mode=%v", m.mode)
	}
	m, _ = press(m, "alt+1")
	if !m.stateEd.focused {
		t.Log("returning to the tab keeps the paused state until enter is pressed")
	}
	m, _ = press(m, "enter")
	if !m.stateEd.focused {
		t.Error("enter should resume editing when the canvas is paused")
	}
}

func TestHelpOverlayIsGlobalAndScrolls(t *testing.T) {
	m := connected(t, newFake(t))
	m, _ = press(m, "ctrl+h")
	if !m.helpOpen {
		t.Fatal("ctrl+h should open help")
	}
	fromBrowse := plain(m.View())
	for _, want := range []string{"pLaya help", "Tabs", "alt+1..8", "Everywhere"} {
		if !strings.Contains(fromBrowse, want) {
			t.Errorf("help missing %q", want)
		}
	}
	m, _ = press(m, "G")
	if !strings.Contains(plain(m.View()), "Windowed Run") {
		t.Error("help should scroll to the window section")
	}
	m, _ = press(m, "g")
	m, _ = press(m, "esc")
	m, _ = press(m, "alt+7")
	m, _ = press(m, "ctrl+h")
	if got := plain(m.View()); got != fromBrowse {
		t.Errorf("help should look identical from every tab:\n%s\n---\n%s", fromBrowse, got)
	}
	m, _ = press(m, "j")
	if got := plain(m.View()); got == fromBrowse && len(helpLines(m.width-4)) > m.helpPage() {
		t.Error("j should scroll the help overlay")
	}
}

func TestQuitIsBlockedInDeleteConfirmation(t *testing.T) {
	snippetHome(t)
	if _, err := snippet.Save("keep", snippet.Snippet{Preset: "guard", StateKey: "prompt", State: map[string]any{"prompt": "x"}}); err != nil {
		t.Fatal(err)
	}
	m := connected(t, newFake(t))
	m, _ = press(m, "alt+6")
	m, _ = press(m, "d")
	if m.snippets.pendingDelete == "" {
		t.Fatal("d should ask for confirmation")
	}
	m, cmd := press(m, "q")
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("q must not quit from the delete confirmation")
		}
	}
	if m.snippets.pendingDelete != "" {
		t.Error("q should cancel the delete confirmation")
	}
}

func TestBackgroundBenchContinuesAcrossTabs(t *testing.T) {
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
		t.Fatal("enter should start the benchmark")
	}
	m, _ = press(m, "alt+0")
	if m.mode != screenBrowse {
		t.Fatalf("alt+0 should go home, mode=%v", m.mode)
	}
	if !m.tabRunning(4) {
		t.Error("the Bench tab should be marked while its job runs")
	}
	m = drain(t, m, cmd)
	if m.bench.running || m.bench.done != benchMinRuns {
		t.Fatalf("background benchmark did not finish: running=%v done=%d", m.bench.running, m.bench.done)
	}
	if strings.Contains(plain(m.View()), "Bench*") {
		t.Error("the running marker should clear when the job is done")
	}
}

func TestBrowseRunMarkerStaysOnHomeTab(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	// Start a run but do not step it, so it stays in flight.
	m, _ = press(m, "enter")
	if !m.running || m.runTab != 0 {
		t.Fatalf("run should start on the home tab: running=%v runTab=%d", m.running, m.runTab)
	}
	m, _ = press(m, "alt+7")
	if m.mode != screenHistory {
		t.Fatalf("alt+7 should open History, mode=%v", m.mode)
	}
	if !m.tabRunning(0) {
		t.Error("the home tab should be marked while its run is in flight")
	}
	if m.tabRunning(7) {
		t.Error("History must not borrow the home tab's in-flight run")
	}
	out := plain(m.View())
	if !strings.Contains(out, "[pLaya]*") || strings.Contains(out, "History*") {
		t.Errorf("tab bar marker is wrong:\n%s", out)
	}
	// Going home and escaping cancels the home run, not anything else.
	m, _ = press(m, "alt+0")
	m, _ = press(m, "esc")
	if m.running {
		t.Fatal("esc on the home tab should cancel its run")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[len(m.toasts)-1].text, "cancelled") {
		t.Errorf("toasts = %+v, want a cancellation message", m.toasts)
	}
}

func TestHistoryReplayMarkerStaysOnHistoryTab(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	if m.running {
		t.Fatal("the recording run should have finished")
	}

	m, _ = press(m, "alt+7")
	m, _ = press(m, "enter")
	if !m.running || m.runTab != 7 {
		t.Fatalf("replay should start on the History tab: running=%v runTab=%d", m.running, m.runTab)
	}
	m, _ = press(m, "alt+0")
	if m.mode != screenBrowse {
		t.Fatalf("alt+0 should go home, mode=%v", m.mode)
	}
	if !m.tabRunning(7) {
		t.Error("History should be marked while its replay is in flight")
	}
	if m.tabRunning(0) {
		t.Error("the home tab must not borrow History's in-flight replay")
	}
	out := plain(m.View())
	if !strings.Contains(out, "History*") || strings.Contains(out, "[pLaya]*") {
		t.Errorf("tab bar marker is wrong:\n%s", out)
	}
	// A bare esc on the home tab must not cancel History's replay.
	m, _ = press(m, "esc")
	if !m.running {
		t.Fatal("esc on the home tab should leave History's replay running")
	}
	m, _ = press(m, "alt+7")
	m, _ = press(m, "esc")
	if m.running {
		t.Fatal("esc on the History tab should cancel its replay")
	}
	if len(m.toasts) == 0 || !strings.Contains(m.toasts[len(m.toasts)-1].text, "cancelled the in-flight replay") {
		t.Errorf("toasts = %+v, want a replay cancellation message", m.toasts)
	}
}
