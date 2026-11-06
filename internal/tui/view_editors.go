package tui

import (
	"fmt"
	"strings"
)

func (m Model) viewState() string {
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	e, d := m.stateEd, m.currentDraft()
	if e == nil || d == nil {
		return "state editor unavailable"
	}

	header := titleStyle.Render("State Canvas") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  %s  ·  model %s", d.preset, d.stateKey, m.modelLabel()))
	meter := m.lengthMeter(d)
	prefix := header + "\n" + meter
	if line := m.routeLine(); line != "" {
		prefix += "\n" + line
	}

	var info string
	if e.err != "" {
		info = errStyle.Render(e.err)
	} else if e.status != "" {
		info = dimStyle.Render(e.status)
	}

	bodyH := h - countLines(prefix) - countLines(info) - 2
	if bodyH < 3 {
		bodyH = 3
	}

	var body string
	if e.form != nil {
		body = e.form.View()
	} else {
		switch e.sub {
		case stateSubRaw:
			body = e.raw.View()
		case stateSubFields:
			body = e.viewFields(d, bodyH)
		default:
			body = e.area.View()
		}
	}

	help := "esc back · ctrl+s apply"
	switch {
	case e.form != nil:
		help = "enter next · shift+tab back · esc cancel"
	case e.sub == stateSubRaw:
		help = "ctrl+s parse and apply · esc cancel"
	case e.sub == stateSubFields:
		help = "j/k move · a add · e edit · d delete · tab text · ctrl+r raw · esc back"
	default:
		help = "tab fields · ctrl+r raw JSON · ctrl+f model · ctrl+s apply · esc back"
	}
	footer := dimStyle.Render(truncate(help, w))

	body = clipLines(body, bodyH)
	if info != "" {
		return strings.Join([]string{prefix, body, info, footer}, "\n")
	}
	return strings.Join([]string{prefix, body, footer}, "\n")
}

func (e *stateEditor) viewFields(d *draft, maxLines int) string {
	fields := d.extraFields()
	var b strings.Builder
	b.WriteString(titleStyle.Render("Other fields") + dimStyle.Render(fmt.Sprintf("  (%d)", len(fields))) + "\n")
	if len(fields) == 0 {
		b.WriteString(dimStyle.Render("  none — press a to add one\n"))
		b.WriteString(dimStyle.Render("  structured values can be edited as JSON in the raw tab\n"))
		return b.String()
	}
	lines := make([]string, len(fields))
	for i, k := range fields {
		line := fmt.Sprintf("%-16s = %s", k, truncate(fieldString(d.state[k]), 70))
		if i == e.cursor {
			lines[i] = selectedStyle.Render("▶ " + line)
		} else {
			lines[i] = "  " + line
		}
	}
	b.WriteString(strings.Join(windowLines(lines, e.cursor, maxLines-3), "\n"))
	return b.String()
}

func (m Model) viewForge() string {
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	e, d := m.forgeEd, m.currentDraft()
	if e == nil || d == nil {
		return "question forge unavailable"
	}

	header := titleStyle.Render("Question Forge") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  %d questions  ·  model %s", d.preset, len(d.questions), m.modelLabel()))

	var info string
	if e.err != "" {
		info = errStyle.Render(e.err)
	} else if e.status != "" {
		info = dimStyle.Render(e.status)
	}

	bodyH := h - countLines(header) - countLines(info) - 2
	if bodyH < 3 {
		bodyH = 3
	}

	var body string
	if e.form != nil {
		body = e.form.View()
	} else {
		switch e.sub {
		case forgeSubRaw:
			body = e.raw.View()
		case forgeSubPreview:
			body = e.preview.View()
		default:
			body = e.viewQuestions(d, bodyH)
		}
	}

	help := "j/k move · n new · e edit · d delete · J/K reorder · p preview · r raw · ctrl+s save · ctrl+o load · esc back"
	switch {
	case e.form != nil && e.formKind == forgeFormQuestion:
		help = "enter next · ctrl+j newline in instructions · shift+tab back · esc cancel"
	case e.form != nil:
		help = "enter next · shift+tab back · esc cancel"
	case e.sub == forgeSubRaw:
		help = "ctrl+s parse and apply · esc cancel"
	case e.sub == forgeSubPreview:
		help = "↑/↓ pgup/pgdn scroll · p or esc back"
	}
	footer := dimStyle.Render(truncate(help, w))

	body = clipLines(body, bodyH)
	if info != "" {
		return strings.Join([]string{header, body, info, footer}, "\n")
	}
	return strings.Join([]string{header, body, footer}, "\n")
}

func (e *forgeEditor) viewQuestions(d *draft, maxLines int) string {
	if len(d.questions) == 0 {
		return dimStyle.Render("No questions yet. Press n to add one.")
	}
	lines := make([]string, len(d.questions))
	for i, nq := range d.questions {
		line := fmt.Sprintf("%2d  %-18s %-6s %s", i+1, truncate(nq.Name, 18), nq.Question.Type, questionSummary(nq))
		if i == e.cursor {
			lines[i] = selectedStyle.Render("▶ " + line)
		} else {
			lines[i] = "  " + line
		}
	}
	return strings.Join(windowLines(lines, e.cursor, maxLines), "\n")
}

// windowLines keeps the selected line visible in a short pane.
func windowLines(lines []string, cursor, max int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	start := cursor - max/2
	if start < 0 {
		start = 0
	}
	if start+max > len(lines) {
		start = len(lines) - max
	}
	return lines[start : start+max]
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
