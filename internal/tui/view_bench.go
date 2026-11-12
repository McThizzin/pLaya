package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/bench"
)

func (m Model) viewBench() string {
	e := m.bench
	if e == nil {
		return "benchmark unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	device := e.device
	if device == "" {
		device = "unknown device"
	}
	header := titleStyle.Render("Benchmark") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  model %s  ·  %s", e.preset, familyLabel(e.model), device))
	if e.status != "" {
		header += dimStyle.Render("  ·  " + truncate(oneLine(e.status), 40))
	}
	parts := []string{header}
	if e.err != "" {
		parts = append(parts, errStyle.Render(truncate(oneLine(e.err), w)))
	}
	if m.benchErr != "" {
		parts = append(parts, errStyle.Render(truncate(oneLine(m.benchErr), w)))
	}
	forwards := (len(e.questions) + benchQuestionChunk - 1) / benchQuestionChunk
	config := fmt.Sprintf("Questions %d · longest ~%d tokens · %d forward(s) · warmup %d · runs %d",
		len(e.questions), e.tokens, forwards, e.warmup, e.runs)
	parts = append(parts, dimStyle.Render(truncate(config, w)))

	bodyH := max(4, h-len(parts))
	var body string
	if e.running {
		body = m.viewBenchRunning(e, w)
	} else {
		body = m.viewBenchResults(e, w)
	}
	parts = append(parts, lipgloss.NewStyle().Height(bodyH).Render(clipLines(body, bodyH)))
	return strings.Join(parts, "\n")
}

const benchQuestionChunk = 8

func (m Model) viewBenchRunning(e *benchSession, w int) string {
	total := e.warmup + e.runs
	done := (e.warmup - e.warmupLeft) + e.done
	frac := 0.0
	if total > 0 {
		frac = float64(done) / float64(total)
	}
	barW := min(36, max(8, w-24))
	var b strings.Builder
	b.WriteString(titleStyle.Render("Running") + "  " + bar(frac, barW) +
		fmt.Sprintf("  %d/%d\n", done, total))
	if len(e.latencies) > 0 {
		s := bench.Summarize(e.latencies, len(e.questions), e.inputTokens, e.outputTokens)
		b.WriteString(dimStyle.Render(fmt.Sprintf(
			"warm p50 %.0fms · p95 %.0fms · %.0f ms/q · %s tok/s · %s q/s",
			s.P50MS, s.P95MS, s.MSPerQuestion, compact(s.TokensPerSec), compact(s.QuestionsPerSec))) + "\n")
	} else if e.warmupLeft > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf(
			"warming up (%d left); warm-up calls are not counted", e.warmupLeft)) + "\n")
	}
	if e.family != "" || e.route != "" {
		b.WriteString(dimStyle.Render(truncate(fmt.Sprintf("family %s · route %s", e.family, e.route), w)) + "\n")
	}
	b.WriteString("\n" + dimStyle.Render("esc cancels; completed warm runs are kept") + "\n")
	return b.String()
}

func (m Model) viewBenchResults(e *benchSession, w int) string {
	if len(e.results) == 0 {
		return titleStyle.Render("No benchmark yet") + "\n\n" +
			dimStyle.Render("enter runs the current questions") + "\n" +
			dimStyle.Render("f cycles the family · [ ] change runs · w changes warmup") + "\n" +
			dimStyle.Render("warm-up calls are discarded; saved results live under the user config dir") + "\n"
	}
	n := min(benchKeepResults, len(e.results))
	panelW := w
	if n > 1 {
		panelW = (w - 2) / 2
	}
	if n > 1 && panelW < 30 {
		n, panelW = 1, w
	}
	panels := make([]string, n)
	for i := 0; i < n; i++ {
		panels[i] = benchPanel(e.results[i], panelW, i == 0)
	}
	if n == 1 {
		return panels[0]
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panels[0], "  ", panels[1])
}

func benchPanel(r bench.Result, w int, latest bool) string {
	s := r.Stats
	name := familyLabel(r.Model)
	if r.Family != "" && r.Family != name {
		name += " → " + r.Family
	}
	title := boldStyle.Render(name)
	if latest {
		title += dimStyle.Render("  latest")
	}
	content := []string{title}
	lines := []string{
		fmt.Sprintf("%s · %d warm runs", r.At.Format("02 Jan 15:04"), r.Runs),
		fmt.Sprintf("p50 %.0fms   p95 %.0fms", s.P50MS, s.P95MS),
		fmt.Sprintf("min %.0fms   max %.0fms", s.MinMS, s.MaxMS),
		fmt.Sprintf("mean %.0fms · %.0f ms/q", s.MeanMS, s.MSPerQuestion),
		fmt.Sprintf("%d in / %d out", s.InputTokens, s.OutputTokens),
		fmt.Sprintf("%s tok/s · %s q/s", compact(s.TokensPerSec), compact(s.QuestionsPerSec)),
		fmt.Sprintf("%d q · ~%d tok/seq", r.Questions, r.Tokens),
	}
	if r.Warmup > 0 {
		lines = append(lines, fmt.Sprintf("warmup %d", r.Warmup))
	}
	inner := max(10, w-4)
	for _, line := range lines {
		content = append(content, dimStyle.Render(truncate(line, inner)))
	}
	return lipgloss.NewStyle().
		Width(w-2).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent).
		Render(strings.Join(content, "\n"))
}
