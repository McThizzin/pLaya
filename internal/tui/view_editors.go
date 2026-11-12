package tui

import (
	"fmt"
	"strings"
)

func (m Model) viewState() string {
	h := m.height
	e, d := m.stateEd, m.currentDraft()
	if e == nil || d == nil {
		return "state editor unavailable"
	}

	focus := "editing"
	if e.form != nil {
		focus = "form"
	} else if e.sub == stateSubRaw {
		focus = "raw JSON"
	} else if e.sub == stateSubFields {
		focus = "fields"
	} else if !e.focused {
		focus = "paused · enter to edit"
	}
	header := titleStyle.Render("State Canvas") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  %s  ·  model %s  ·  %s",
			d.preset, d.stateKey, m.modelLabel(), focus))
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

	bodyH := h - countLines(prefix) - countLines(info)
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

	body = clipLines(body, bodyH)
	if info != "" {
		return strings.Join([]string{prefix, body, info}, "\n")
	}
	return strings.Join([]string{prefix, body}, "\n")
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
	h := m.height
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

	bodyH := h - countLines(header) - countLines(info)
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

	body = clipLines(body, bodyH)
	if info != "" {
		return strings.Join([]string{header, body, info}, "\n")
	}
	return strings.Join([]string{header, body}, "\n")
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
