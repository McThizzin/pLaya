package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/snippet"
)

// snippetSession is a library of reusable states. The screen is a name list on
// the left and a preview of the selected state on the right.
type snippetSession struct {
	names         []string
	cursor        int
	raw           bool
	prompt        bool
	input         textinput.Model
	pendingDelete string
	status        string
	err           string
	returnTo      screen
	width, height int
}

func newSnippetSession(returnTo screen, width, height int) *snippetSession {
	input := textinput.New()
	input.Placeholder = "snippet name"
	input.CharLimit = 512
	input.Prompt = "save as › "
	e := &snippetSession{input: input, returnTo: returnTo}
	e.resize(width, height)
	e.reload("")
	return e
}

func (e *snippetSession) resize(width, height int) {
	e.width, e.height = width, height
	e.input.Width = max(20, width-14)
}

// reload re-reads the directory and keeps the cursor on selectSlug when it is
// still present.
func (e *snippetSession) reload(selectSlug string) {
	names, err := snippet.All()
	if err != nil {
		e.err = err.Error()
		return
	}
	e.err = ""
	e.names = names
	if selectSlug != "" {
		for i, name := range names {
			if name == selectSlug {
				e.cursor = i
				return
			}
		}
	}
	if e.cursor >= len(names) {
		e.cursor = max(0, len(names)-1)
	}
}

func (m Model) openSnippets(returnTo screen) (tea.Model, tea.Cmd) {
	// Coming from the State Canvas, make sure the latest textarea text is in
	// the draft before the save prompt can run.
	if returnTo == screenState && m.stateEd != nil {
		if d := m.currentDraft(); d != nil && m.stateEd.sub == stateSubText {
			d.setStateText(m.stateEd.area.Value())
		}
	}
	m.snippets = newSnippetSession(returnTo, m.width, m.height)
	m.mode = screenSnippets
	return m, nil
}

func (e *snippetSession) selected() (string, bool) {
	if e.cursor < 0 || e.cursor >= len(e.names) {
		return "", false
	}
	return e.names[e.cursor], true
}

func (m Model) handleSnippetKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e, d := m.snippets, m.currentDraft()
	if e == nil || d == nil {
		m.mode = screenBrowse
		return m, nil
	}
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	if e.prompt {
		switch key {
		case "esc":
			e.prompt = false
			e.input.Blur()
			e.status = ""
			return m, nil
		case "enter":
			name := strings.TrimSpace(e.input.Value())
			e.prompt = false
			e.input.Blur()
			if snippet.Slug(name) == "" {
				e.err = "a name is required"
				return m, nil
			}
			path, err := snippet.Save(name, snippet.Snippet{
				Preset:   d.preset,
				StateKey: d.stateKey,
				State:    cloneState(d.state),
			})
			if err != nil {
				e.err = err.Error()
				return m, nil
			}
			e.err = ""
			e.raw = false
			e.reload(snippet.Slug(name))
			e.status = "saved " + path
			return m, nil
		}
		var cmd tea.Cmd
		e.input, cmd = e.input.Update(msg)
		return m, cmd
	}

	if e.pendingDelete != "" {
		switch key {
		case "y":
			name := e.pendingDelete
			e.pendingDelete = ""
			if err := snippet.Delete(name); err != nil {
				e.err = err.Error()
				return m, nil
			}
			e.err = ""
			e.reload("")
			e.status = "deleted " + name
			return m, nil
		case "esc":
			e.pendingDelete = ""
			e.status = "delete cancelled"
			return m, nil
		default:
			e.pendingDelete = ""
			return m, nil
		}
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		if e.returnTo == screenState && (m.stateEd == nil || m.currentDraft() == nil) {
			m.mode = screenBrowse
		} else {
			m.mode = e.returnTo
		}
		e.status = ""
		return m, nil
	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}
		e.raw = false
	case "down", "j":
		if e.cursor < len(e.names)-1 {
			e.cursor++
		}
		e.raw = false
	case "g", "home":
		e.cursor = 0
	case "G", "end":
		if len(e.names) > 0 {
			e.cursor = len(e.names) - 1
		}
	case "enter":
		name, ok := e.selected()
		if !ok {
			return m, nil
		}
		s, err := snippet.Load(name)
		if err != nil {
			e.err = err.Error()
			return m, nil
		}
		d.state = cloneState(s.State)
		d.stateKey = s.StateKey
		if _, ok := d.state[d.stateKey]; !ok {
			d.state[d.stateKey] = ""
		}
		d.dirty = true
		m.invalidate(d.preset)
		m.refreshStateEditor(d)
		e.err = ""
		e.raw = false
		from := ""
		if s.Preset != "" && s.Preset != d.preset {
			from = " (saved from " + s.Preset + ")"
		}
		e.status = fmt.Sprintf("loaded %s into %s%s", name, d.preset, from)
		m, cmd := m.queueRoute(d, d.stateText())
		return m, cmd
	case "s":
		e.prompt = true
		e.err = ""
		e.status = ""
		name := snippet.Slug(d.preset)
		if name == "" {
			name = "snippet"
		}
		e.input.SetValue(name)
		e.input.CursorEnd()
		return m, e.input.Focus()
	case "d":
		if name, ok := e.selected(); ok {
			e.pendingDelete = name
			e.status = "delete " + name + "? press y to confirm"
		}
	case "v":
		if len(e.names) > 0 {
			e.raw = !e.raw
		}
	case "ctrl+r":
		e.reload("")
		e.status = fmt.Sprintf("%d saved snippets", len(e.names))
	}
	return m, nil
}

// refreshStateEditor shows a loaded snippet in the text tab. Any old raw text
// is stale after the draft's state is replaced.
func (m *Model) refreshStateEditor(d *draft) {
	if m.stateEd == nil {
		return
	}
	m.stateEd.area.SetValue(d.stateText())
	m.stateEd.area.CursorEnd()
	m.stateEd.sub = stateSubText
	m.stateEd.cursor = 0
	m.stateEd.err = ""
	m.stateEd.area.Focus()
	if m.stateEd.form != nil {
		m.stateEd.form = nil
	}
}

// selectedSnippet loads the highlighted snippet for the preview pane.
func (e *snippetSession) selectedSnippet() (snippet.Snippet, string, error) {
	name, ok := e.selected()
	if !ok {
		return snippet.Snippet{}, "", fmt.Errorf("no snippet selected")
	}
	s, err := snippet.Load(name)
	return s, name, err
}

// snippetRaw pretty-prints a snippet for the raw toggle.
func snippetRaw(s snippet.Snippet) string {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}
