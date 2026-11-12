package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const windowListWidth = 34

func (m Model) viewWindow() string {
	e := m.windowed
	if e == nil {
		return "windowed run unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}

	header := titleStyle.Render("Windowed Run") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  key %s  ·  model %s",
			e.preset, e.stateKey, familyLabel(e.model)))
	parts := []string{header}
	if e.status != "" {
		parts = append(parts, dimStyle.Render(truncate(oneLine(e.status), w)))
	}
	if e.err != "" {
		parts = append(parts, errStyle.Render(truncate(oneLine(e.err), w)))
	}

	bodyH := h - len(parts)
	if bodyH < 4 {
		bodyH = 4
	}
	if len(e.windows) == 0 {
		parts = append(parts, lipgloss.NewStyle().Height(bodyH).Render(
			dimStyle.Render("This state is empty; press esc and type some text.")))
	} else {
		rightW := w - windowListWidth - 2
		left := clipLines(e.viewWindowList(bodyH), bodyH)
		right := clipLines(e.viewWindowDetail(rightW), bodyH)
		body := lipgloss.NewStyle().Height(bodyH).Render(
			lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().Width(windowListWidth).Render(left), "  ",
				lipgloss.NewStyle().Width(rightW).Render(right)))
		parts = append(parts, clipLines(body, bodyH))
	}
	out := strings.Join(parts, "\n")
	if h > 0 {
		out = clipLines(out, h)
	}
	return out
}

func (e *windowSession) viewWindowList(bodyH int) string {
	max := bodyH - 4 // title, config, blank
	if max < 1 {
		max = 1
	}
	lines := make([]string, len(e.windows))
	for i, w := range e.windows {
		star := "  "
		if i == e.strongest {
			star = "★ "
		}
		status := "—"
		if r := e.results[i]; r.Resp != nil || r.Err != nil {
			if r.Err != nil || r.Resp == nil {
				status = "err"
			} else if mean, ok := meanWinner(r.Resp, e.questions); ok {
				status = fmt.Sprintf("%.2f", mean)
			}
		}
		line := fmt.Sprintf("%s#%-2d %4d-%-4d %3dt %s", star, i+1, w.Start, w.End, w.Tokens, status)
		if i == e.cursor {
			lines[i] = selectedStyle.Render("▶ " + line)
		} else {
			lines[i] = "  " + line
		}
	}
	visible := windowLines(lines, clamp(e.cursor, 0, len(lines)-1), max)
	config := fmt.Sprintf("%d windows · ~%d tokens · %d%% overlap",
		len(e.windows), e.size, int(e.overlapPct*100))
	return titleStyle.Render("Windows") + "\n" +
		dimStyle.Render(truncate(config, windowListWidth)) + "\n\n" +
		strings.Join(visible, "\n")
}

func (e *windowSession) viewWindowDetail(width int) string {
	if e.cursor < 0 || e.cursor >= len(e.windows) {
		return dimStyle.Render("no window selected")
	}
	w := e.windows[e.cursor]
	var b strings.Builder
	b.WriteString(boldStyle.Render(fmt.Sprintf("Window #%d", e.cursor+1)) +
		dimStyle.Render(fmt.Sprintf("  runes %d-%d · ~%d tokens", w.Start, w.End, w.Tokens)))
	if e.cursor == e.strongest {
		b.WriteString("  " + fg(colGreen, "★ strongest"))
	}
	b.WriteString("\n\n")

	r := e.results[e.cursor]
	if e.raw {
		if r.Resp != nil {
			b.WriteString(truncate(r.Resp.PrettyRaw(), 6000))
		} else if r.Err != nil {
			b.WriteString(errStyle.Render(r.Err.Error()))
		} else {
			b.WriteString(dimStyle.Render("No result yet. Press enter to run all windows."))
		}
		return b.String()
	}

	if r.Err != nil {
		b.WriteString(errStyle.Render(truncate(oneLine(r.Err.Error()), width)) + "\n\n")
	} else if r.Resp != nil {
		mean, ok := meanWinner(r.Resp, e.questions)
		if ok {
			b.WriteString(dimStyle.Render(fmt.Sprintf("mean winner probability %.2f · family %s",
				mean, familyLabel(r.Resp.Family))) + "\n\n")
		}
		b.WriteString(renderDecisions(e.preset, summarize(e.questions, r.Resp), width, false))
		b.WriteString("\n\n")
	}
	b.WriteString(titleStyle.Render("Text") + "\n")
	b.WriteString(truncate(w.Text, max(20, width*7)))
	return b.String()
}
