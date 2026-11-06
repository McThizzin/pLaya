package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/history"
)

const historyListWidth = 34

func (m Model) viewHistory() string {
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	entries := m.history.List()
	header := titleStyle.Render("History") + dimStyle.Render(fmt.Sprintf("  ·  %d runs", len(entries)))
	if m.historyErr != "" {
		header += dimStyle.Render("  ·  " + truncate(oneLine(m.historyErr), 50))
	}

	bodyH := h - 2 // header, footer
	if bodyH < 4 {
		bodyH = 4
	}

	var body string
	if len(entries) == 0 {
		body = lipgloss.NewStyle().Height(bodyH).Render(
			dimStyle.Render("No runs yet. Press esc to go back and run a preset."))
	} else {
		listW := historyListWidth
		rightW := w - listW - 2
		left := clipLines(m.viewHistoryList(entries, bodyH), bodyH)
		e, _ := m.selectedHistory()
		right := clipLines(m.viewHistoryDetail(e, rightW), bodyH)
		body = lipgloss.NewStyle().Height(bodyH).Render(
			lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(listW).Render(left), "  ",
				lipgloss.NewStyle().Width(rightW).Render(right)))
		body = clipLines(body, bodyH)
	}

	help := "j/k move · enter replay · v raw · esc back · ctrl+c quit"
	switch {
	case m.running:
		help = "esc cancel · replaying…"
	case len(entries) == 0:
		help = "esc back · ctrl+c quit"
	case m.historyRaw:
		help = "j/k move · enter replay · v decisions · esc back"
	}
	footer := dimStyle.Render(truncate(help, w))
	return strings.Join([]string{header, body, footer}, "\n")
}

func (m Model) viewHistoryList(entries []history.Entry, bodyH int) string {
	max := bodyH - 2 // title + blank
	if max < 1 {
		max = 1
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		status := "ok"
		if e.Err != "" {
			status = "err"
		} else if e.Cold {
			status = "cold"
		}
		marker := ""
		if e.ReplayOf > 0 {
			marker = " ↻"
		}
		stamp := "        "
		if !e.At.IsZero() {
			stamp = e.At.Format("15:04:05")
		}
		line := fmt.Sprintf("#%-3d %-10s %s %-4s%s", e.ID, truncate(e.Preset, 10), stamp, status, marker)
		if i == m.historyCursor {
			lines[i] = selectedStyle.Render("▶ " + line)
		} else {
			lines[i] = "  " + line
		}
	}
	visible := windowLines(lines, clamp(m.historyCursor, 0, len(lines)-1), max)
	return titleStyle.Render("Runs") + "\n\n" + strings.Join(visible, "\n")
}

func (m Model) viewHistoryDetail(e history.Entry, width int) string {
	var b strings.Builder
	modelName := e.Model
	if modelName == "" {
		modelName = "auto"
	}
	stamp := "unknown time"
	if !e.At.IsZero() {
		stamp = e.At.Format("2006-01-02 15:04:05")
	}
	b.WriteString(boldStyle.Render(fmt.Sprintf("#%d %s", e.ID, e.Preset)) +
		dimStyle.Render(fmt.Sprintf("  ·  model %s  ·  %s", modelName, stamp)) + "\n")
	if e.ReplayOf > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("replay of #%d", e.ReplayOf)) + "\n")
	}
	if e.Cold {
		b.WriteString(dimStyle.Render("cold call (excluded from medians)") + "\n")
	}

	if e.Err != "" {
		b.WriteString("\n" + errStyle.Render(e.Err) + "\n")
		if hint := paramHint(e.Param); hint != "" {
			b.WriteString(dimStyle.Render(hint) + "\n")
		}
		b.WriteString("\n" + viewHistoryRequest(e, width))
		return b.String()
	}
	if e.Response == nil {
		b.WriteString("\n" + dimStyle.Render("No response stored.") + "\n\n")
		b.WriteString(viewHistoryRequest(e, width))
		return b.String()
	}

	if m.historyRaw {
		raw := []byte(e.Raw)
		if len(raw) == 0 {
			raw, _ = json.Marshal(e.Response)
		}
		b.WriteString("\n" + titleStyle.Render("Raw response") + "\n" + prettyJSON(raw))
		return b.String()
	}

	r := e.Response
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("%s  ·  %s", r.Family, r.Route)) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("%d in / %d out tokens  ·  %.0f ms server  ·  %.0f ms wall",
		r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.LatencyMs, e.WallMs)) + "\n\n")
	b.WriteString(renderDecisions(e.Preset, summarize(e.Request.Questions, r), width, entryTruncated(e)))
	return b.String()
}

func viewHistoryRequest(e history.Entry, width int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Request") + "\n")
	state := fieldString(e.Request.State)
	b.WriteString(dimStyle.Render("state: ") + truncate(strings.ReplaceAll(state, "\n", " "), max(40, width-7)) + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("questions (%d): ", len(e.Request.Questions))) +
		truncate(strings.Join(e.Request.Questions.Names(), ", "), max(20, width-15)) + "\n")
	return b.String()
}
