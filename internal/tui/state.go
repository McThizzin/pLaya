package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"playa/internal/api"
)

type stateSub int

const (
	stateSubText stateSub = iota
	stateSubFields
	stateSubRaw
)

// stateEditor is the State Canvas. The main text lives in a textarea; other
// fields are edited one at a time with a small huh form, and the raw tab edits
// the whole state object as JSON.
type stateEditor struct {
	sub     stateSub
	area    textarea.Model
	raw     textarea.Model
	cursor  int
	form    *huh.Form
	editKey string // field being edited; empty means a new field
	keyVal  string
	valVal  string
	err     string
	status  string
	width   int
	height  int
}

func newStateEditor(d *draft, width, height int) *stateEditor {
	area := textarea.New()
	area.ShowLineNumbers = false
	area.Prompt = ""
	area.CharLimit = 0
	area.SetValue(d.stateText())

	raw := textarea.New()
	raw.ShowLineNumbers = true
	raw.Prompt = ""
	raw.CharLimit = 0

	e := &stateEditor{area: area, raw: raw}
	e.resize(width, height)
	e.area.Focus()
	return e
}

func (e *stateEditor) resize(width, height int) {
	e.width, e.height = width, height
	body := height - 8
	if body < 4 {
		body = 4
	}
	w := width - 4
	if w < 20 {
		w = 20
	}
	e.area.SetWidth(w)
	e.area.SetHeight(body)
	e.raw.SetWidth(w)
	e.raw.SetHeight(body)
	if e.form != nil {
		e.form.WithWidth(w).WithHeight(body)
	}
}

func (m Model) openStateEditor() (tea.Model, tea.Cmd) {
	p, ok := m.selected()
	if !ok {
		return m, nil
	}
	d := m.draftFor(p)
	m.stateEd = newStateEditor(d, m.width, m.height)
	m.mode = screenState
	m, routeCmd := m.queueRoute(d, d.stateText())
	return m, tea.Batch(textarea.Blink, routeCmd)
}

func (m Model) handleStateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e, d := m.stateEd, m.currentDraft()
	if e == nil || d == nil {
		m.mode = screenBrowse
		return m, nil
	}
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	if e.form != nil {
		if key == "esc" {
			e.form = nil
			e.err = ""
			return m, nil
		}
		model, cmd := e.form.Update(msg)
		e.form = model.(*huh.Form)
		if e.form.State == huh.StateCompleted {
			return m.finishFieldForm(d)
		}
		return m, cmd
	}

	// These open another screen from any tab whose edits are already live.
	// Raw JSON is the exception: applying it first would discard a parse error.
	if e.sub != stateSubRaw {
		switch key {
		case "ctrl+l":
			if e.sub == stateSubText {
				d.setStateText(e.area.Value())
			}
			return m.openSnippets(screenState)
		case "ctrl+w":
			if e.sub == stateSubText {
				d.setStateText(e.area.Value())
			}
			return m.openWindow()
		}
	}

	switch e.sub {
	case stateSubRaw:
		switch key {
		case "esc":
			e.sub, e.err = stateSubText, ""
			e.area.Focus()
			return m, nil
		case "ctrl+s":
			if err := e.applyRaw(d); err != nil {
				e.err = err.Error()
				return m, nil
			}
			m.invalidate(d.preset)
			e.status, e.err = "state applied", ""
			m, cmd := m.queueRoute(d, d.stateText())
			return m, cmd
		}
		var cmd tea.Cmd
		e.raw, cmd = e.raw.Update(msg)
		return m, cmd

	case stateSubFields:
		fields := d.extraFields()
		switch key {
		case "up", "k":
			if e.cursor > 0 {
				e.cursor--
			}
		case "down", "j":
			if e.cursor < len(fields)-1 {
				e.cursor++
			}
		case "g", "home":
			e.cursor = 0
		case "G", "end":
			if len(fields) > 0 {
				e.cursor = len(fields) - 1
			}
		case "a", "n":
			return m.openFieldForm(d, "")
		case "e", "enter":
			if len(fields) > 0 {
				return m.openFieldForm(d, fields[clamp(e.cursor, 0, len(fields)-1)])
			}
		case "d", "delete":
			if len(fields) > 0 {
				removed := fields[clamp(e.cursor, 0, len(fields)-1)]
				d.deleteField(removed)
				m.invalidate(d.preset)
				e.status = "removed " + removed
				if e.cursor >= len(fields)-1 && e.cursor > 0 {
					e.cursor--
				}
			}
		case "tab":
			e.sub = stateSubText
			e.area.Focus()
		case "ctrl+r":
			e.enterRaw(d)
		case "esc", "ctrl+s":
			m.mode = screenBrowse
			return m, nil
		}
		return m, nil
	}

	// main text
	switch key {
	case "esc", "ctrl+s":
		d.setStateText(e.area.Value())
		m.invalidate(d.preset)
		m.mode = screenBrowse
		return m, nil
	case "tab":
		d.setStateText(e.area.Value())
		m.invalidate(d.preset)
		e.sub = stateSubFields
		e.cursor = 0
		return m, nil
	case "ctrl+r":
		d.setStateText(e.area.Value())
		m.invalidate(d.preset)
		e.enterRaw(d)
		return m, nil
	case "ctrl+f":
		m.cycleModel()
		return m, nil
	}

	before := e.area.Value()
	var cmd tea.Cmd
	e.area, cmd = e.area.Update(msg)
	if e.area.Value() != before {
		d.setStateText(e.area.Value())
		m.invalidate(d.preset)
		e.status = ""
		var routeCmd tea.Cmd
		m, routeCmd = m.queueRoute(d, e.area.Value())
		return m, tea.Batch(cmd, routeCmd)
	}
	return m, cmd
}

func (e *stateEditor) enterRaw(d *draft) {
	b, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		e.err = err.Error()
		return
	}
	e.raw.SetValue(string(b))
	e.raw.Focus()
	e.cursor = 0
	e.sub = stateSubRaw
	e.err = ""
}

func (e *stateEditor) applyRaw(d *draft) error {
	var state map[string]any
	if err := json.Unmarshal([]byte(e.raw.Value()), &state); err != nil {
		return err
	}
	if state == nil {
		state = map[string]any{}
	}
	d.state = state
	if _, ok := d.state[d.stateKey]; !ok {
		// keep the main text field present so the text tab can keep editing
		d.state[d.stateKey] = ""
	}
	d.dirty = true
	e.area.SetValue(d.stateText())
	e.sub = stateSubText
	e.area.Focus()
	return nil
}

func (m Model) openFieldForm(d *draft, key string) (tea.Model, tea.Cmd) {
	e := m.stateEd
	e.editKey = key
	e.keyVal = key
	e.valVal = ""
	if key != "" {
		e.valVal = fieldString(d.state[key])
	}
	nameValidate := func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return errors.New("field name is required")
		}
		if s == d.stateKey {
			return fmt.Errorf("%q is the main text field", s)
		}
		if s != e.editKey {
			if _, exists := d.state[s]; exists {
				return fmt.Errorf("%q already exists", s)
			}
		}
		return nil
	}
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Field name").Value(&e.keyVal).Validate(nameValidate),
		huh.NewInput().Title("Value (JSON is allowed)").Value(&e.valVal),
	)).WithWidth(e.width - 4).WithHeight(e.height - 8)
	e.err = ""
	return m, e.form.Init()
}

func (m Model) finishFieldForm(d *draft) (tea.Model, tea.Cmd) {
	e := m.stateEd
	key := strings.TrimSpace(e.keyVal)
	if key == "" {
		e.err = "field name is required"
		return m, nil
	}
	if d.state == nil {
		d.state = map[string]any{}
	}
	if e.editKey != "" && e.editKey != key {
		delete(d.state, e.editKey)
	}
	value := any(e.valVal)
	// A value that is valid JSON is stored as JSON; otherwise as text.
	if trimmed := strings.TrimSpace(e.valVal); trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed == "true" || trimmed == "false" || trimmed == "null" || (trimmed[0] >= '0' && trimmed[0] <= '9') || trimmed[0] == '-') {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			value = decoded
		}
	}
	d.state[key] = value
	d.dirty = true
	e.form = nil
	e.editKey = ""
	e.sub = stateSubFields
	e.err = ""
	e.status = "saved " + key
	m.invalidate(d.preset)
	return m, nil
}

func fieldString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// draftFor returns the editable draft for p, creating it if needed.
func (m Model) draftFor(p api.Preset) *draft {
	if m.drafts == nil {
		return newDraft(p)
	}
	if d, ok := m.drafts[p.Name]; ok && d != nil {
		return d
	}
	d := newDraft(p)
	m.drafts[p.Name] = d
	return d
}

func (m Model) currentDraft() *draft {
	p, ok := m.selected()
	if !ok {
		return nil
	}
	return m.draftFor(p)
}

// invalidate clears the result shown for a preset after its draft changes.
func (m *Model) invalidate(preset string) {
	if m.resultOf == preset {
		m.result = nil
		m.resultOf = ""
		m.summaries = nil
		m.truncated = false
		m.showRaw = false
		m.runErr = ""
		m.runErrParam = ""
	}
}
