package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/api"
	"playa/internal/lens"
)

func (m Model) viewAB() string {
	e := m.ab
	if e == nil {
		return "A/B compare unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}

	header := titleStyle.Render("A/B Compare") + dimStyle.Render("  ·  "+e.preset)
	if e.status != "" {
		header += dimStyle.Render("  ·  " + truncate(oneLine(e.status), 52))
	}
	parts := []string{header}
	parts = append(parts, m.abTargetLines(e, w)...)

	bodyH := max(4, h-len(parts))
	body := m.abBody(e, w)
	parts = append(parts, lipgloss.NewStyle().Height(bodyH).Render(clipLines(body, bodyH)))
	return strings.Join(parts, "\n")
}

func (m Model) abTargetLines(e *abSession, w int) []string {
	out := make([]string, 0, 2)
	for i, t := range e.targets {
		marker := "  "
		style := dimStyle
		if e.selected == i {
			marker = "▸ "
			style = selectedStyle
		}
		if e.editing && e.selected == i {
			out = append(out, marker+abName(i)+" URL  "+e.input.View())
			continue
		}
		head := fmt.Sprintf("%s%s  model %s", marker, abName(i), familyLabel(t.Model))
		line := style.Render(truncate(head, w))
		if t.URL != "" {
			line += dimStyle.Render("  @ " + truncate(t.URL, max(8, w-len([]rune(head))-4)))
		}
		out = append(out, line)
	}
	return out
}

func (m Model) abBody(e *abSession, w int) string {
	if e.running {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Running both targets") + "\n\n")
		b.WriteString(dimStyle.Render("The two calls run concurrently. esc cancels both.") + "\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("%d questions · model A %s · model B %s",
			len(e.questions), familyLabel(e.targets[0].Model), familyLabel(e.targets[1].Model))) + "\n")
		return b.String()
	}
	if !e.ran {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Ready") + "\n\n")
		b.WriteString(dimStyle.Render("enter runs the current draft against both targets.") + "\n")
		b.WriteString(dimStyle.Render("tab selects a target, m cycles its family, u edits its URL.") + "\n")
		b.WriteString(dimStyle.Render("Use two URLs to compare builds or quants on different ports.") + "\n")
		return b.String()
	}
	return m.abResults(e, w)
}

func (m Model) abResults(e *abSession, w int) string {
	a := abSummaries(e.questions, e.results[0].Resp)
	b := abSummaries(e.questions, e.results[1].Resp)
	diffs := abDiffIndexes(a, b)
	diffSet := make(map[int]bool, len(diffs))
	for _, i := range diffs {
		diffSet[i] = true
	}

	nameW := 4
	for _, nq := range e.questions {
		if n := len([]rune(nq.Name)); n > nameW {
			nameW = n
		}
	}
	nameW = min(nameW, 18)

	var body string
	if w >= 80 {
		panelW := (w - 2) / 2
		left := abPanel("A", e.targets[0], e.results[0], e.questions, a, diffSet, nameW, panelW)
		right := abPanel("B", e.targets[1], e.results[1], e.questions, b, diffSet, nameW, panelW)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	} else {
		body = abPanel("A", e.targets[0], e.results[0], e.questions, a, diffSet, nameW, w) +
			"\n" + abPanel("B", e.targets[1], e.results[1], e.questions, b, diffSet, nameW, w)
	}

	if e.results[0].Resp == nil || e.results[1].Resp == nil {
		return body + "\n\n" + dimStyle.Render("A comparison needs both results.")
	}
	body += "\n\n" + m.abDiffText(e, a, b, diffs, w)
	return body
}

func abPanel(label string, t abTarget, out abOutcome, qs api.QuestionSet, sums []lens.Summary, diffs map[int]bool, nameW, width int) string {
	var b strings.Builder
	title := label + " · " + familyLabel(t.Model)
	if out.Resp != nil && out.Resp.Family != "" {
		title += " → " + out.Resp.Family
	}
	b.WriteString(boldStyle.Render(truncate(title, width)) + "\n")

	if out.Err != nil {
		b.WriteString(errStyle.Render(truncate(oneLine(out.Err.Error()), width)) + "\n")
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}
	if out.Resp == nil {
		b.WriteString(dimStyle.Render("no result") + "\n")
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	meta := fmt.Sprintf("server %.0fms · wall %dms · %s",
		out.Resp.Usage.LatencyMs, out.Wall.Milliseconds(), out.Resp.Route)
	b.WriteString(dimStyle.Render(truncate(meta, width)) + "\n")
	tokens := fmt.Sprintf("%d in · %d out · %d q",
		out.Resp.Usage.InputTokens, out.Resp.Usage.OutputTokens, len(qs))
	b.WriteString(dimStyle.Render(truncate(tokens, width)) + "\n\n")

	for i, s := range sums {
		markerStyled := " "
		if diffs[i] {
			markerStyled = fg(colAmber, "≠")
		}
		labelText := "—"
		if s.Label != "" {
			labelText = shortLabel(s.Label)
		}
		ansW := max(6, width-nameW-8)
		ans := fmt.Sprintf("%-*s %3.0f%%", ansW, truncate(labelText, ansW), 100*s.Prob)
		if s.Label == "" {
			ans = fmt.Sprintf("%-*s", ansW+4, "—")
		}
		line := markerStyled + " " + fmt.Sprintf("%-*s", nameW, truncate(qs[i].Name, nameW)) + " "
		if s.Label == "" {
			line += dimStyle.Render(ans)
		} else {
			line += lipgloss.NewStyle().Foreground(probColor(s.Prob)).Bold(true).Render(ans)
		}
		b.WriteString(line + "\n")
	}
	return lipgloss.NewStyle().Width(width).Render(strings.TrimRight(b.String(), "\n"))
}

func (m Model) abDiffText(e *abSession, a, b []lens.Summary, diffs []int, width int) string {
	if len(diffs) == 0 {
		return dimStyle.Render(fmt.Sprintf("Winners agree on all %d questions.", len(e.questions)))
	}
	nameW := 4
	for _, i := range diffs {
		if n := len([]rune(e.questions[i].Name)); n > nameW {
			nameW = n
		}
	}
	nameW = min(nameW, 20)

	var out strings.Builder
	out.WriteString(titleStyle.Render(fmt.Sprintf("Differences  ·  %d of %d", len(diffs), len(e.questions))) + "\n")
	for _, i := range diffs {
		line := fmt.Sprintf("  %-*s  A %s  ≠  B %s",
			nameW, truncate(e.questions[i].Name, nameW), abAnswer(a[i]), abAnswer(b[i]))
		out.WriteString(dimStyle.Render(truncate(line, width)) + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}
