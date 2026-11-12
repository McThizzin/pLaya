package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"playa/internal/api"
	"playa/internal/forge"
)

type forgeSub int

const (
	forgeSubList forgeSub = iota
	forgeSubForm
	forgeSubRaw
	forgeSubPreview
)

type forgeFormKind int

const (
	forgeFormNone forgeFormKind = iota
	forgeFormType
	forgeFormQuestion
	forgeFormSave
	forgeFormLoad
)

// forgeEditor is the Question Forge. The list is the home screen; forms are
// built per question type, and raw mode edits the whole question set as JSON.
type forgeEditor struct {
	sub      forgeSub
	cursor   int
	form     *huh.Form
	formKind forgeFormKind
	editIdx  int
	qtype    string

	name, instructions      string
	optionsText, levelsText string
	falseDesc, trueDesc     string
	saveName, loadName      string
	loadNames               []string

	preview viewport.Model
	raw     textarea.Model
	err     string
	status  string
	preset  string // draft this editor was built for
	width   int
	height  int
}

func newForgeEditor(d *draft, width, height int) *forgeEditor {
	raw := textarea.New()
	raw.ShowLineNumbers = true
	raw.Prompt = ""
	raw.CharLimit = 0

	e := &forgeEditor{raw: raw, preview: viewport.New(80, 20), preset: d.preset}
	e.resize(width, height)
	if len(d.questions) > 0 {
		e.cursor = 0
	}
	return e
}

func (e *forgeEditor) resize(width, height int) {
	e.width, e.height = width, height
	body := height - 8
	if body < 4 {
		body = 4
	}
	w := width - 4
	if w < 20 {
		w = 20
	}
	e.raw.SetWidth(w)
	e.raw.SetHeight(body)
	e.preview.Width, e.preview.Height = w, body
	if e.form != nil {
		e.form.WithWidth(w).WithHeight(min(body, 26))
	}
}

func (m Model) openForge() (tea.Model, tea.Cmd) {
	p, ok := m.selected()
	if !ok {
		return m, nil
	}
	d := m.draftFor(p)
	m.forgeEd = newForgeEditor(d, m.bodyW(), m.bodyH())
	if name := questionParam(m.runErrParam); name != "" {
		for i, nq := range d.questions {
			if nq.Name == name {
				m.forgeEd.cursor = i
				break
			}
		}
	}
	m.mode = screenForge
	return m, nil
}

func (m Model) handleForgeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	e, d := m.forgeEd, m.currentDraft()
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
			e.form, e.formKind = nil, forgeFormNone
			e.err = ""
			e.sub = forgeSubList
			return m, nil
		}
		model, cmd := e.form.Update(msg)
		e.form = model.(*huh.Form)
		if e.form.State == huh.StateCompleted {
			return m.finishForgeForm(d)
		}
		return m, cmd
	}

	switch e.sub {
	case forgeSubRaw:
		switch key {
		case "esc":
			e.sub = forgeSubList
			e.err = ""
			return m, nil
		case "ctrl+s":
			if err := e.applyRaw(d); err != nil {
				e.err = err.Error()
				return m, nil
			}
			m.invalidate(d.preset)
			e.status, e.err = "questions applied", ""
			return m, nil
		}
		var cmd tea.Cmd
		e.raw, cmd = e.raw.Update(msg)
		return m, cmd

	case forgeSubPreview:
		switch key {
		case "esc", "p":
			e.sub = forgeSubList
			return m, nil
		}
		var cmd tea.Cmd
		e.preview, cmd = e.preview.Update(msg)
		return m, cmd
	}

	// list
	switch key {
	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}
	case "down", "j":
		if e.cursor < len(d.questions)-1 {
			e.cursor++
		}
	case "g", "home":
		e.cursor = 0
	case "G", "end":
		if len(d.questions) > 0 {
			e.cursor = len(d.questions) - 1
		}
	case "enter", "e":
		if len(d.questions) > 0 {
			nq := d.questions[clamp(e.cursor, 0, len(d.questions)-1)]
			return m.openQuestionForm(d, e.cursor, nq.Question.Type)
		}
	case "n":
		return m.openTypeForm(d)
	case "d", "delete":
		if len(d.questions) > 0 {
			i := clamp(e.cursor, 0, len(d.questions)-1)
			name := d.questions[i].Name
			d.questions = append(d.questions[:i], d.questions[i+1:]...)
			d.dirty = true
			m.invalidate(d.preset)
			e.status = "removed " + name
			if e.cursor >= len(d.questions) && e.cursor > 0 {
				e.cursor--
			}
		}
	case "J":
		if e.cursor < len(d.questions)-1 {
			d.questions[e.cursor], d.questions[e.cursor+1] = d.questions[e.cursor+1], d.questions[e.cursor]
			e.cursor++
			d.dirty = true
			m.invalidate(d.preset)
		}
	case "K":
		if e.cursor > 0 {
			d.questions[e.cursor], d.questions[e.cursor-1] = d.questions[e.cursor-1], d.questions[e.cursor]
			e.cursor--
			d.dirty = true
			m.invalidate(d.preset)
		}
	case "p":
		e.enterPreview(d, m.model)
	case "r":
		e.enterRaw(d)
	case "ctrl+s":
		return m.openSaveForm(d)
	case "ctrl+o":
		return m.openLoadForm(d)
	case "esc":
		m.mode = screenBrowse
		return m, nil
	}
	return m, nil
}

func (m Model) openTypeForm(d *draft) (tea.Model, tea.Cmd) {
	e := m.forgeEd
	e.qtype = api.TypeNoul
	e.formKind = forgeFormType
	e.sub = forgeSubForm
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("New question type").
			Options(huh.NewOptions(api.TypeNoul, api.TypeChoice, api.TypeScore)...).
			Value(&e.qtype).
			Height(3),
	)).WithWidth(e.width - 4).WithHeight(min(e.height-8, 12))
	e.err = ""
	return m, e.form.Init()
}

func (m Model) openQuestionForm(d *draft, idx int, typ string) (tea.Model, tea.Cmd) {
	e := m.forgeEd
	e.formKind = forgeFormQuestion
	e.editIdx = idx
	e.qtype = typ
	e.optionsText, e.levelsText = "", ""
	e.falseDesc, e.trueDesc = "", ""
	e.err = ""

	if idx >= 0 && idx < len(d.questions) {
		nq := d.questions[idx]
		e.name = nq.Name
		e.instructions = nq.Question.Instructions
		switch typ {
		case api.TypeChoice:
			e.optionsText = forge.FormatChoice(nq.Question.Options())
		case api.TypeScore:
			e.levelsText = forge.FormatLevels(nq.Question.Levels())
		case api.TypeNoul:
			for _, o := range nq.Question.Options() {
				switch o.Key {
				case "false":
					e.falseDesc = o.Description
				case "true":
					e.trueDesc = o.Description
				}
			}
		}
	} else {
		e.name = forge.NextName(d.questions, typ)
		e.instructions = ""
		switch typ {
		case api.TypeChoice:
			e.optionsText = "option_a\noption_b"
		case api.TypeScore:
			e.levelsText = "low\nhigh"
		}
	}

	fields := []huh.Field{
		huh.NewInput().Title("Question key").Value(&e.name).Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("a key is required")
			}
			for i, nq := range d.questions {
				if i != idx && nq.Name == s {
					return fmt.Errorf("%q is already used", s)
				}
			}
			return nil
		}),
		huh.NewText().
			Title("Instructions").
			Value(&e.instructions).
			Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("instructions are required")
				}
				return nil
			}).
			WithHeight(5),
	}
	switch typ {
	case api.TypeChoice:
		fields = append(fields, huh.NewText().
			Title("Options — one per line, key: description").
			Description(fmt.Sprintf("Up to %d. Order is preserved.", forge.MaxOptions)).
			Value(&e.optionsText).
			Validate(func(s string) error {
				_, err := forge.ParseChoice(s)
				return err
			}).
			WithHeight(8))
	case api.TypeScore:
		fields = append(fields, huh.NewText().
			Title("Levels — one per line, low to high").
			Description(fmt.Sprintf("At least %d. A level's position is its score.", forge.MinLevels)).
			Value(&e.levelsText).
			Validate(func(s string) error {
				_, err := forge.ParseLevels(s)
				return err
			}).
			WithHeight(8))
	case api.TypeNoul:
		fields = append(fields,
			huh.NewInput().Title("Description for FALSE (optional)").Value(&e.falseDesc),
			huh.NewInput().Title("Description for TRUE (optional)").Value(&e.trueDesc),
		)
	}

	e.sub = forgeSubForm
	e.form = huh.NewForm(huh.NewGroup(fields...)).
		WithWidth(e.width - 4).
		WithHeight(min(e.height-8, 28))
	return m, e.form.Init()
}

func (m Model) finishForgeForm(d *draft) (tea.Model, tea.Cmd) {
	e := m.forgeEd
	kind := e.formKind
	e.form, e.formKind = nil, forgeFormNone
	e.sub = forgeSubList

	switch kind {
	case forgeFormType:
		return m.openQuestionForm(d, -1, e.qtype)
	case forgeFormSave:
		if strings.TrimSpace(e.saveName) == "" {
			e.err = "a name is required"
			return m, nil
		}
		path, err := forge.Save(e.saveName, forge.Set{Preset: d.preset, Questions: d.questions})
		if err != nil {
			e.err = err.Error()
			return m, nil
		}
		e.err = ""
		e.status = "saved " + path
		return m, nil
	case forgeFormLoad:
		set, err := forge.Load(e.loadName)
		if err != nil {
			e.err = err.Error()
			return m, nil
		}
		d.questions = set.Questions
		d.dirty = true
		m.invalidate(d.preset)
		e.cursor = 0
		e.err = ""
		e.status = fmt.Sprintf("loaded %s (%d questions)", e.loadName, len(set.Questions))
		return m, nil
	}

	// question form
	var opts []api.Option
	var levels []string
	var err error
	switch e.qtype {
	case api.TypeChoice:
		opts, err = forge.ParseChoice(e.optionsText)
	case api.TypeScore:
		levels, err = forge.ParseLevels(e.levelsText)
	case api.TypeNoul:
		if e.falseDesc != "" || e.trueDesc != "" {
			opts = []api.Option{{Key: "false", Description: e.falseDesc}, {Key: "true", Description: e.trueDesc}}
		}
	}
	if err == nil {
		var q api.Question
		q, err = forge.Question(e.qtype, e.instructions, opts, levels)
		if err == nil {
			name := strings.TrimSpace(e.name)
			if name == "" {
				err = errors.New("a key is required")
			} else {
				nq := api.NamedQuestion{Name: name, Question: q}
				if e.editIdx >= 0 && e.editIdx < len(d.questions) {
					d.questions[e.editIdx] = nq
					e.cursor = e.editIdx
				} else {
					d.questions = append(d.questions, nq)
					e.cursor = len(d.questions) - 1
				}
				d.dirty = true
				m.invalidate(d.preset)
				e.err = ""
				e.status = "saved " + name
				return m, nil
			}
		}
	}
	e.err = err.Error()
	return m, nil
}

func (e *forgeEditor) enterRaw(d *draft) {
	b, err := json.MarshalIndent(d.questions, "", "  ")
	if err != nil {
		e.err = err.Error()
		return
	}
	e.raw.SetValue(string(b))
	e.raw.Focus()
	e.sub = forgeSubRaw
	e.err = ""
}

func (e *forgeEditor) applyRaw(d *draft) error {
	var qs api.QuestionSet
	if err := json.Unmarshal([]byte(e.raw.Value()), &qs); err != nil {
		return err
	}
	if len(qs) == 0 {
		return errors.New("a question set needs at least one question")
	}
	for _, nq := range qs {
		if strings.TrimSpace(nq.Name) == "" {
			return errors.New("every question needs a key")
		}
		switch nq.Question.Type {
		case api.TypeNoul, api.TypeChoice, api.TypeScore:
		default:
			return fmt.Errorf("%s: unknown type %q", nq.Name, nq.Question.Type)
		}
	}
	d.questions = qs
	d.dirty = true
	e.sub = forgeSubList
	return nil
}

func (e *forgeEditor) enterPreview(d *draft, model string) {
	b, err := json.MarshalIndent(d.request(model), "", "  ")
	if err != nil {
		e.err = err.Error()
		return
	}
	e.preview.SetContent(string(b))
	e.preview.GotoTop()
	e.sub = forgeSubPreview
	e.err = ""
}

func (m Model) openSaveForm(d *draft) (tea.Model, tea.Cmd) {
	e := m.forgeEd
	e.formKind = forgeFormSave
	e.sub = forgeSubForm
	e.saveName = forge.Slug(d.preset)
	if e.saveName == "" {
		e.saveName = "questions"
	}
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Save question set as").
			Description("Stored under the playa config directory.").
			Value(&e.saveName).
			Validate(func(s string) error {
				if forge.Slug(s) == "" {
					return errors.New("a name is required")
				}
				return nil
			}),
	)).WithWidth(e.width - 4).WithHeight(8)
	e.err = ""
	return m, e.form.Init()
}

func (m Model) openLoadForm(d *draft) (tea.Model, tea.Cmd) {
	e := m.forgeEd
	names, err := forge.List()
	if err != nil {
		e.err = err.Error()
		return m, nil
	}
	if len(names) == 0 {
		e.status = "no saved question sets yet"
		return m, nil
	}
	e.loadNames = names
	e.loadName = names[0]
	e.formKind = forgeFormLoad
	e.sub = forgeSubForm
	e.form = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Load question set").
			Options(huh.NewOptions(names...)...).
			Value(&e.loadName).
			Height(min(len(names), 8)),
	)).WithWidth(e.width - 4).WithHeight(min(e.height-8, 14))
	e.err = ""
	return m, e.form.Init()
}

func questionSummary(nq api.NamedQuestion) string {
	switch nq.Question.Type {
	case api.TypeChoice:
		opts := nq.Question.Options()
		names := make([]string, 0, len(opts))
		for _, o := range opts {
			names = append(names, o.Key)
		}
		return fmt.Sprintf("%d options: %s", len(opts), truncate(strings.Join(names, ", "), 60))
	case api.TypeScore:
		levels := nq.Question.Levels()
		return fmt.Sprintf("%d levels: %s", len(levels), truncate(strings.Join(levels, " / "), 60))
	case api.TypeNoul:
		opts := nq.Question.Options()
		if len(opts) == 0 {
			return "yes / no"
		}
		return "yes / no with descriptions"
	}
	return nq.Question.Type
}
