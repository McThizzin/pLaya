package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/lengthguard"
)

const snippetListWidth = 30

func (m Model) viewSnippets() string {
	e := m.snippets
	if e == nil {
		return "snippet library unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	preset := ""
	if d := m.currentDraft(); d != nil {
		preset = d.preset
	}
	header := titleStyle.Render("Snippets") +
		dimStyle.Render(fmt.Sprintf("  ·  %d saved  ·  load into %s", len(e.names), preset))

	parts := []string{header}
	if e.prompt {
		parts = append(parts, e.input.View())
	}
	if e.status != "" {
		parts = append(parts, dimStyle.Render(truncate(oneLine(e.status), w)))
	}
	if e.err != "" {
		parts = append(parts, errStyle.Render(truncate(oneLine(e.err), w)))
	}

	bodyH := h - len(parts) - 1 // footer
	if bodyH < 4 {
		bodyH = 4
	}
	var body string
	if len(e.names) == 0 {
		body = lipgloss.NewStyle().Height(bodyH).Render(
			dimStyle.Render("No snippets yet. Press s to save the current state."))
	} else {
		rightW := w - snippetListWidth - 2
		left := clipLines(e.viewList(bodyH), bodyH)
		right := clipLines(e.viewDetail(rightW), bodyH)
		body = lipgloss.NewStyle().Height(bodyH).Render(
			lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(snippetListWidth).Render(left), "  ",
				lipgloss.NewStyle().Width(rightW).Render(right)))
		body = clipLines(body, bodyH)
	}
	parts = append(parts, body)
	parts = append(parts, dimStyle.Render(truncate(m.snippetHelp(e), w)))

	out := strings.Join(parts, "\n")
	if h > 0 {
		out = clipLines(out, h)
	}
	return out
}

func (e *snippetSession) viewList(bodyH int) string {
	max := bodyH - 2 // title + blank
	if max < 1 {
		max = 1
	}
	lines := make([]string, len(e.names))
	for i, name := range e.names {
		if i == e.cursor {
			lines[i] = selectedStyle.Render("▶ " + truncate(name, snippetListWidth-3))
		} else {
			lines[i] = "  " + truncate(name, snippetListWidth-3)
		}
	}
	visible := windowLines(lines, clamp(e.cursor, 0, len(lines)-1), max)
	return titleStyle.Render("Saved") + "\n\n" + strings.Join(visible, "\n")
}

func (e *snippetSession) viewDetail(width int) string {
	s, name, err := e.selectedSnippet()
	if err != nil {
		return titleStyle.Render(name) + "\n\n" + errStyle.Render(truncate(err.Error(), width))
	}
	if e.raw {
		return titleStyle.Render(name) + "\n\n" + truncate(snippetRaw(s), 4000)
	}

	var b strings.Builder
	b.WriteString(boldStyle.Render(name) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("from %s  ·  key %s  ·  ~%d tokens  ·  %d field(s)",
		labelOr(s.Preset, "unknown preset"), s.StateKey,
		lengthguard.EstimateTokens(fieldString(s.State[s.StateKey])), len(s.State))) + "\n\n")

	b.WriteString(titleStyle.Render("State") + "\n")
	text := fieldString(s.State[s.StateKey])
	if text == "" {
		text = "(empty)"
	}
	b.WriteString(truncate(text, max(20, width*6)) + "\n")

	extra := make([]string, 0, len(s.State))
	for k := range s.State {
		if k != s.StateKey {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		b.WriteString("\n" + titleStyle.Render("Other fields") + "\n")
		for _, k := range extra {
			b.WriteString(dimStyle.Render(fmt.Sprintf("%-14s = %s", k, truncate(fieldString(s.State[k]), max(8, width-18)))) + "\n")
		}
	}
	return b.String()
}

func (m Model) snippetHelp(e *snippetSession) string {
	switch {
	case e.prompt:
		return "enter save · esc cancel"
	case e.pendingDelete != "":
		return "y confirm delete · esc cancel"
	case len(e.names) == 0:
		return "s save current state · esc back · ctrl+c quit"
	case e.raw:
		return "j/k move · v preview · enter load · s save · d delete · esc back"
	}
	return "j/k move · enter load · s save · d delete · v raw · ctrl+r refresh · esc back"
}

func labelOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
