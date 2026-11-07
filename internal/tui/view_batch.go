package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/api"
	"playa/internal/batch"
	"playa/internal/lens"
)

const (
	batchLabelWidth    = 30
	batchQuestionWidth = 14
)

func (m Model) viewBatch() string {
	e := m.batch
	if e == nil {
		return "batch unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}
	modelName := e.model
	if modelName == "" || modelName == "auto" {
		modelName = "auto"
	}
	header := titleStyle.Render("Batch") +
		dimStyle.Render(fmt.Sprintf("  ·  %s  ·  %s  ·  model %s", e.preset, batchFileLabel(e.path), modelName))
	if e.status != "" {
		header += dimStyle.Render("  ·  " + truncate(oneLine(e.status), 48))
	}

	parts := []string{header}
	if e.loadErr != "" {
		parts = append(parts, errStyle.Render(truncate(oneLine(e.loadErr), w)))
	}

	switch {
	case e.prompt:
		bodyH := max(2, h-len(parts)-1)
		body := "Open a file of states.\n\n" +
			"JSONL: one string or object per line, or CSV: one column per state field.\n\n" +
			e.input.View()
		parts = append(parts, clipLines(body, bodyH))
		parts = append(parts, dimStyle.Render(truncate("enter load · esc cancel", w)))
	case e.detail:
		bodyH := max(2, h-len(parts)-1)
		parts = append(parts, clipLines(m.viewBatchDetail(e, w), bodyH))
		parts = append(parts, dimStyle.Render(truncate("esc back · v raw · q quit", w)))
	default:
		bodyH := max(3, h-len(parts)-2)
		body := clipLines(m.viewBatchTable(e, w, bodyH), bodyH)
		parts = append(parts, lipgloss.NewStyle().Height(bodyH).Render(body))
		parts = append(parts, m.batchSummaryLine(e, w))
		parts = append(parts, dimStyle.Render(truncate(m.batchHelp(e), w)))
	}
	return strings.Join(parts, "\n")
}

func batchFileLabel(path string) string {
	if path == "" {
		return "no file"
	}
	return filepath.Base(path)
}

func (m Model) viewBatchTable(e *batchSession, w, bodyH int) string {
	if len(e.rows) == 0 {
		var b strings.Builder
		b.WriteString(titleStyle.Render("States") + "\n\n")
		b.WriteString(dimStyle.Render("Press ctrl+o to open a .jsonl or .csv file.") + "\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("Each state is sent through %s's %d questions (%d per request).",
			e.preset, len(e.questions), batch.DefaultChunkSize)) + "\n")
		return b.String()
	}
	order := m.batchOrder()
	if len(order) == 0 {
		return titleStyle.Render("States") + "\n\n" +
			dimStyle.Render("No rows match filter "+e.filter.String()+".") + "\n"
	}

	labelW := batchLabelWidth
	qW := batchQuestionWidth
	if w < 100 {
		labelW, qW = 22, 13
	}
	room := w - labelW - 2
	visible := room / qW
	if visible < 1 {
		visible = 1
	}
	off := clamp(e.colOffset, 0, max(0, len(e.questions)-visible))
	e.colOffset = off
	end := min(len(e.questions), off+visible)

	var title strings.Builder
	title.WriteString(titleStyle.Render("States"))
	var bits []string
	if e.filter != batch.FilterAll {
		bits = append(bits, "filter "+e.filter.String())
	}
	if e.sortCol >= 0 {
		dir := "asc"
		if e.sortDesc {
			dir = "desc"
		}
		bits = append(bits, fmt.Sprintf("sort %s %s", e.questions[e.sortCol].Name, dir))
	}
	if off > 0 || end < len(e.questions) {
		bits = append(bits, fmt.Sprintf("columns %d-%d/%d", off+1, end, len(e.questions)))
	}
	if len(bits) > 0 {
		title.WriteString(dimStyle.Render("  ·  " + strings.Join(bits, " · ")))
	}

	header := "  " + dimStyle.Render(padTo("state", labelW))
	for _, q := range e.questions[off:end] {
		header += " " + dimStyle.Render(padTo(q.Name, qW))
	}

	rows := make([]string, len(order))
	for pos, idx := range order {
		r := e.rows[idx]
		marker := "  "
		if pos == e.cursor {
			marker = selectedStyle.Render("▶ ")
		}
		label := fmt.Sprintf("#%d %s", r.Index, r.Label)
		line := marker + padTo(label, labelW)
		for _, q := range e.questions[off:end] {
			line += " " + m.batchCell(r, q, qW)
		}
		rows[pos] = line
	}
	maxRows := bodyH - 2 // title + table header
	if maxRows < 1 {
		maxRows = 1
	}
	shown := windowLines(rows, clamp(e.cursor, 0, len(rows)-1), maxRows)
	return strings.Join([]string{title.String(), header, strings.Join(shown, "\n")}, "\n")
}

func (m Model) batchCell(r batch.Row, q api.NamedQuestion, width int) string {
	a, ok := r.Answers()[q.Name]
	if !ok {
		if r.Failed() {
			return fg(colRed, padTo("err", width))
		}
		return dimStyle.Render(padTo("…", width))
	}
	s := lens.Summarize(q.Name, a, lens.Options{})
	text := fmt.Sprintf("%s %3.0f%%", shortLabel(s.Label), 100*s.Prob)
	return fg(probColor(s.Prob), padTo(text, width))
}

func (m Model) batchSummaryLine(e *batchSession, w int) string {
	s := batch.Summarize(e.rows, len(e.chunks), e.threshold)
	parts := []string{fmt.Sprintf("%d/%d rows", s.Complete, s.Rows)}
	if s.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d err", s.Failed))
	}
	if s.Calls > 0 {
		parts = append(parts, fmt.Sprintf("p50 %.0fms", s.P50), fmt.Sprintf("p95 %.0fms", s.P95))
		parts = append(parts, fmt.Sprintf("%.0f tok/s", s.TokensPerSec), fmt.Sprintf("%.1f rows/s", s.RowsPerSec))
	}
	if s.NoulTotal > 0 {
		parts = append(parts, fmt.Sprintf("noul+ %.0f%% @%.2f",
			100*float64(s.NoulPositive)/float64(s.NoulTotal), e.threshold))
	}
	if len(s.Choices) > 0 {
		keys := make([]string, 0, len(s.Choices))
		for k := range s.Choices {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var counts []string
		for _, k := range keys {
			counts = append(counts, fmt.Sprintf("%s=%d", k, s.Choices[k]))
		}
		parts = append(parts, "choice "+strings.Join(counts, " "))
	}
	if s.Truncated > 0 {
		parts = append(parts, fmt.Sprintf("%d truncated", s.Truncated))
	}
	return dimStyle.Render(truncate(strings.Join(parts, " │ "), w))
}

func (m Model) batchHelp(e *batchSession) string {
	if e.running {
		return "esc cancel · running… · q quit"
	}
	if len(e.rows) == 0 {
		return "ctrl+o open file · esc back · q quit"
	}
	return "j/k rows · h/l columns · enter detail · r run/resume · R rerun · f filter · s sort · S dir · t threshold · ctrl+o open · x export · esc back · q quit"
}

func (m Model) viewBatchDetail(e *batchSession, w int) string {
	r, _, ok := m.selectedBatch()
	if !ok {
		return dimStyle.Render("No row selected.")
	}
	var b strings.Builder
	label := r.Label
	if label == "" {
		label = oneLine(batchStateText(r.State))
	}
	b.WriteString(boldStyle.Render(fmt.Sprintf("#%d %s", r.Index, label)) + "\n\n")

	b.WriteString(titleStyle.Render("State") + "\n")
	b.WriteString(batchStatePreview(r.State) + "\n")

	if len(r.Calls) > 0 {
		b.WriteString("\n" + titleStyle.Render("Calls") + "\n")
		for i, c := range r.Calls {
			if i >= 8 {
				b.WriteString(dimStyle.Render(fmt.Sprintf("… %d more chunks", len(r.Calls)-8)) + "\n")
				break
			}
			if c.Err != "" {
				b.WriteString(errStyle.Render(fmt.Sprintf("chunk %d: %s", c.Chunk+1, c.Err)) + "\n")
				if hint := paramHint(c.Param); hint != "" {
					b.WriteString(dimStyle.Render(hint) + "\n")
				}
				continue
			}
			if c.Response == nil {
				continue
			}
			resp := c.Response
			b.WriteString(dimStyle.Render(fmt.Sprintf("chunk %d: %s · %s · %d in / %d out · %.0f ms",
				c.Chunk+1, resp.Family, resp.Route, resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.LatencyMs)) + "\n")
		}
	}

	if e.raw {
		b.WriteString("\n" + titleStyle.Render("Raw response") + "\n" + batchRaw(r) + "\n")
		return b.String()
	}

	answers := r.Answers()
	if len(answers) > 0 {
		b.WriteString("\n" + renderDecisions(e.preset, summarizeAnswers(e.questions, answers), w, r.Truncated()))
	} else if !r.Failed() {
		b.WriteString("\n" + dimStyle.Render("No answers yet.") + "\n")
	}
	return b.String()
}

func summarizeAnswers(qs api.QuestionSet, answers map[string]api.Answer) []lens.Summary {
	var out []lens.Summary
	for _, nq := range qs {
		if a, ok := answers[nq.Name]; ok {
			out = append(out, lens.Summarize(nq.Name, a, lens.Options{}))
		}
	}
	return out
}

func batchStateText(state any) string {
	if s, ok := state.(string); ok {
		return s
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Sprint(state)
	}
	return string(b)
}

// batchStatePreview keeps a long state from pushing the decisions off-screen.
func batchStatePreview(state any) string {
	s := batchStateText(state)
	lines := strings.Split(s, "\n")
	if len(lines) <= 8 {
		return s
	}
	return strings.Join(lines[:8], "\n") + "\n" + dimStyle.Render(fmt.Sprintf("… %d more lines", len(lines)-8))
}

func batchRaw(r batch.Row) string {
	var raws []json.RawMessage
	for _, c := range r.Calls {
		if c.Response == nil {
			continue
		}
		raw := c.Response.Raw
		if len(raw) == 0 || !json.Valid(raw) {
			raw, _ = json.Marshal(c.Response)
		}
		raws = append(raws, json.RawMessage(raw))
	}
	if len(raws) == 0 {
		return dimStyle.Render("no response stored")
	}
	if len(raws) == 1 {
		return prettyJSON(raws[0])
	}
	b, err := json.MarshalIndent(raws, "", "  ")
	if err != nil {
		return string(raws[0])
	}
	return string(b)
}

// padTo pads or truncates s to exactly n runes.
func padTo(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}
