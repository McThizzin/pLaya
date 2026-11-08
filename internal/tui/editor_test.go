package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/api"
)

func openState(t *testing.T, f *fake) Model {
	t.Helper()
	m := connected(t, f)
	m, _ = press(m, "g")
	next, _ := press(m, "ctrl+e")
	m = next
	if m.mode != screenState || m.stateEd == nil {
		t.Fatalf("state editor not open (mode=%v)", m.mode)
	}
	return m
}

func openForge(t *testing.T, f *fake) Model {
	t.Helper()
	m := connected(t, f)
	m, _ = press(m, "g")
	next, _ := press(m, "Q")
	m = next
	if m.mode != screenForge || m.forgeEd == nil {
		t.Fatalf("forge not open (mode=%v)", m.mode)
	}
	return m
}

func TestStateEditChangesRunPayload(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue("A prompt about a double charge and a refund")
	m, _ = press(m, "ctrl+s")
	if m.mode != screenBrowse {
		t.Fatalf("mode = %v, want browse", m.mode)
	}
	d := m.currentDraft()
	if got := d.stateText(); got != "A prompt about a double charge and a refund" {
		t.Errorf("draft state = %q", got)
	}
	if !m.dirty(m.presets[m.cursor]) {
		t.Error("draft should be marked dirty")
	}

	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	if !strings.Contains(string(f.lastBody), "A prompt about a double charge and a refund") {
		t.Errorf("request did not use the edited state: %s", f.lastBody)
	}
}

func TestStateLengthGuardWarnsBeforeSend(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	d := m.currentDraft()
	long := strings.Repeat("this sentence fills the state ", 120) // ~3500 chars, over 512 tokens
	m.stateEd.area.SetValue(long)
	// a keypress syncs the textarea into the draft
	m, _ = press(m, "x")

	out := plain(m.View())
	for _, want := range []string{"longest sequence", "cut silently", "/ 512"} {
		if !strings.Contains(out, want) {
			t.Errorf("state view missing %q:\n%s", want, out)
		}
	}
	if d.stateText() == "" {
		t.Error("typing should update the draft immediately")
	}
}

func TestStateRawJSONApply(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m, _ = press(m, "ctrl+r")
	if m.stateEd.sub != stateSubRaw {
		t.Fatal("ctrl+r should open the raw state editor")
	}
	m.stateEd.raw.SetValue(`{"prompt":"raw text","count":7,"nested":{"a":true}}`)
	m, _ = press(m, "ctrl+s")
	if m.stateEd.sub != stateSubText {
		t.Fatalf("sub = %v, want text", m.stateEd.sub)
	}
	d := m.currentDraft()
	if d.stateText() != "raw text" {
		t.Errorf("prompt = %v", d.state[d.stateKey])
	}
	if d.state["count"] != float64(7) {
		t.Errorf("count = %#v (%T)", d.state["count"], d.state["count"])
	}

	m, _ = press(m, "esc")
	out := plain(m.View())
	if !strings.Contains(out, "raw text") {
		t.Errorf("detail did not show the edited state:\n%s", out)
	}
}

func TestStateFieldDelete(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	d := m.currentDraft()
	d.state["extra"] = "value"
	before := len(d.extraFields())
	if before == 0 {
		t.Fatal("expected at least one extra field")
	}
	m, _ = press(m, "tab")
	if m.stateEd.sub != stateSubFields {
		t.Fatal("tab should open the fields tab")
	}
	m, _ = press(m, "d")
	if got := len(d.extraFields()); got != before-1 {
		t.Errorf("fields = %d, want %d", got, before-1)
	}
}

func TestForgeAddQuestionAndOrder(t *testing.T) {
	f := newFake(t)
	m := openForge(t, f)
	d := m.currentDraft()
	before := len(d.questions)

	next, _ := m.openQuestionForm(d, -1, api.TypeChoice)
	m = next.(Model)
	m.forgeEd.name = "custom"
	m.forgeEd.instructions = "Pick one"
	m.forgeEd.optionsText = "z: last\na: first"
	// Simulate the form having been submitted.
	m.forgeEd.form, m.forgeEd.formKind = nil, forgeFormQuestion
	next, _ = m.finishForgeForm(d)
	m = next.(Model)

	if len(d.questions) != before+1 {
		t.Fatalf("questions = %d, want %d", len(d.questions), before+1)
	}
	got := d.questions[len(d.questions)-1]
	if got.Name != "custom" || got.Question.Type != api.TypeChoice {
		t.Errorf("new question = %+v", got)
	}
	if string(got.Question.Criteria) != `{"z":"last","a":"first"}` {
		t.Errorf("criteria order = %s", got.Question.Criteria)
	}

	m, _ = press(m, "esc")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	body := string(f.lastBody)
	if !strings.Contains(body, `"custom"`) {
		t.Errorf("request missing the new question: %s", body)
	}
	if strings.Index(body, `"z"`) > strings.Index(body, `"a"`) {
		t.Errorf("option order lost in request: %s", body)
	}
}

func TestForgeReorderAndDelete(t *testing.T) {
	f := newFake(t)
	m := openForge(t, f)
	d := m.currentDraft()
	first := d.questions[0].Name
	second := d.questions[1].Name

	m, _ = press(m, "J")
	if d.questions[0].Name != second || d.questions[1].Name != first {
		t.Errorf("J did not move the question: %v", d.questions.Names())
	}
	if m.forgeEd.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.forgeEd.cursor)
	}
	m, _ = press(m, "K")
	if d.questions[0].Name != first || m.forgeEd.cursor != 0 {
		t.Errorf("K did not move it back: %v", d.questions.Names())
	}

	m, _ = press(m, "d")
	for _, nq := range d.questions {
		if nq.Name == first {
			t.Errorf("delete left %q in the set", first)
		}
	}
}

func TestForgeRawJSONKeepsOrder(t *testing.T) {
	f := newFake(t)
	m := openForge(t, f)
	m, _ = press(m, "r")
	m.forgeEd.raw.SetValue(`{"b":{"type":"noul","instructions":"B?"},"a":{"type":"score","instructions":"A?","criteria":["low","high"]}}`)
	m, _ = press(m, "ctrl+s")
	d := m.currentDraft()
	if got := strings.Join(d.questions.Names(), ","); got != "b,a" {
		t.Errorf("order = %s", got)
	}
	if d.questions[1].Question.Type != api.TypeScore {
		t.Errorf("type = %s", d.questions[1].Question.Type)
	}
}

func TestForgeSaveAndLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newFake(t)
	m := openForge(t, f)
	d := m.currentDraft()
	count := len(d.questions)

	next, _ := m.openSaveForm(d)
	m = next.(Model)
	m.forgeEd.saveName = "guard set"
	m.forgeEd.form, m.forgeEd.formKind = nil, forgeFormSave
	next, _ = m.finishForgeForm(d)
	m = next.(Model)
	if !strings.Contains(m.forgeEd.status, "saved") {
		t.Fatalf("save status = %q", m.forgeEd.status)
	}

	d.questions = d.questions[:1]
	next, _ = m.openLoadForm(d)
	m = next.(Model)
	if m.forgeEd.form == nil {
		t.Fatalf("load form did not open: %q", m.forgeEd.status)
	}
	m.forgeEd.form, m.forgeEd.formKind = nil, forgeFormLoad
	next, _ = m.finishForgeForm(d)
	m = next.(Model)
	if len(d.questions) != count {
		t.Errorf("loaded %d questions, want %d", len(d.questions), count)
	}
}

func TestModelCyclePinsRequests(t *testing.T) {
	f := newFake(t)
	m := connected(t, f)
	if m.model != "auto" {
		t.Fatalf("initial model = %q", m.model)
	}
	m, _ = press(m, "f")
	if m.model != "english" {
		t.Errorf("model = %q, want english", m.model)
	}
	m, _ = press(m, "f")
	if m.model != "kev-0.8b" {
		t.Errorf("model = %q, want kev-0.8b", m.model)
	}

	m, _ = press(m, "g")
	f.decideBody = guardBody
	var cmd tea.Cmd
	m, cmd = press(m, "enter")
	m = step(t, m, cmd)
	if !strings.Contains(string(f.lastBody), `"model":"kev-0.8b"`) {
		t.Errorf("request did not pin the model: %s", f.lastBody)
	}
}

func TestResetDraft(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m.stateEd.area.SetValue("changed")
	m, _ = press(m, "ctrl+s")
	if !m.dirty(m.presets[m.cursor]) {
		t.Fatal("draft should be dirty")
	}
	m, _ = press(m, "R")
	if m.dirty(m.presets[m.cursor]) {
		t.Error("R should reset the draft")
	}
	if got := m.currentDraft().stateText(); strings.Contains(got, "changed") {
		t.Errorf("state after reset = %q", got)
	}
}

func TestQuitKeysInEditor(t *testing.T) {
	f := newFake(t)
	m := openState(t, f)
	m, cmd := press(m, "q")
	if m.mode != screenState {
		t.Error("q should be typed, not quit, while editing")
	}
	if cmd != nil {
		// textarea cursor commands are fine; the program must not quit
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("q quit the program from the state editor")
		}
	}
	if !strings.Contains(m.stateEd.area.Value(), "q") {
		t.Error("q was not inserted into the state")
	}

	_, cmd = press(m, "ctrl+c")
	if cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c should still quit")
	}
}

func TestEditorViewsFitTerminal(t *testing.T) {
	f := newFake(t)
	m := openForge(t, f)
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = next.(Model)
		lines := strings.Split(plain(m.View()), "\n")
		if len(lines) > size.h {
			t.Errorf("forge %dx%d: %d lines", size.w, size.h, len(lines))
		}
	}
	m, _ = press(m, "esc")
	m = openState(t, f)
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m = next.(Model)
		lines := strings.Split(plain(m.View()), "\n")
		if len(lines) > size.h {
			t.Errorf("state %dx%d: %d lines", size.w, size.h, len(lines))
		}
	}
}
