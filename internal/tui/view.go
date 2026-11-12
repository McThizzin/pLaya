package tui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/api"
	"playa/internal/lengthguard"
	"playa/internal/lens"
)

const listWidth = 24

// View composes the persistent tab bar, the framed body of the active tab, any
// toast lines, and the tab's key bar.
func (m Model) View() string {
	if m.width < minTermWidth || m.height < minTermHeight {
		return viewTooSmall(m.width, m.height)
	}
	if m.helpOpen {
		return m.viewHelp()
	}

	toasts := m.toastLines(m.width)
	bodyH := m.bodyH() - len(toasts)
	if bodyH < 3 {
		bodyH = 3
		toasts = nil
	}
	frame := boxed(m.viewBody(bodyH), m.width, bodyH, colAccent)

	parts := make([]string, 0, 3+len(toasts))
	parts = append(parts, m.viewTabBar(), frame)
	parts = append(parts, toasts...)
	parts = append(parts, m.viewKeyBar())
	return strings.Join(parts, "\n")
}

// viewBody renders the active tab at the inner body size. The chrome owns the
// header and footer, so tabs must not draw their own.
func (m Model) viewBody(height int) string {
	b := m
	b.width, b.height = m.bodyW(), height
	switch m.mode {
	case screenState:
		return b.viewState()
	case screenForge:
		return b.viewForge()
	case screenHistory:
		return b.viewHistory()
	case screenBatch:
		return b.viewBatch()
	case screenBench:
		return b.viewBench()
	case screenAB:
		return b.viewAB()
	case screenLaunch:
		return b.viewLaunch()
	case screenSnippets:
		return b.viewSnippets()
	case screenWindow:
		return b.viewWindow()
	}
	return b.viewBrowse()
}

// viewBrowse is the pLaya home tab: preset list | detail, with telemetry kept
// inside this tab.
func (m Model) viewBrowse() string {
	w, h := m.width, m.height
	rightW := w - listWidth - 2

	var head string
	switch {
	case m.notice != "":
		head = dimStyle.Render(truncate(m.notice, w))
	case m.status == statusError:
		head = errStyle.Render(truncate("cannot reach server: "+m.statusMsg, w))
	}
	headLines := 0
	if head != "" {
		headLines = 1
	}

	bodyH := h - headLines - 1 // telemetry bar
	if bodyH < 4 {
		bodyH = 4
	}

	left := clipLines(lipgloss.NewStyle().Width(listWidth).Render(m.viewList()), bodyH)
	right := clipLines(lipgloss.NewStyle().Width(rightW).Render(m.viewDetail(rightW)), bodyH)
	body := lipgloss.NewStyle().Height(bodyH).Render(
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(listWidth).Render(left), "  ", right),
	)

	parts := make([]string, 0, 3)
	if head != "" {
		parts = append(parts, head)
	}
	parts = append(parts, body, m.telemetryBar(w))
	return strings.Join(parts, "\n")
}

func (m Model) viewList() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Presets") + "\n\n")
	if len(m.presets) == 0 {
		if m.loadErr != "" {
			b.WriteString(errStyle.Render(m.loadErr))
		} else {
			b.WriteString(dimStyle.Render("loading…"))
		}
		return b.String()
	}
	for i, p := range m.presets {
		line := fmt.Sprintf("[%s] %s", shortcutFor(p.Name), p.Name)
		if m.dirty(p) {
			line += " *"
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("▶ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

func (m Model) viewDetail(width int) string {
	p, ok := m.selected()
	if !ok {
		if m.status == statusError {
			return errStyle.Render(m.statusMsg) + "\n\n" + dimStyle.Render("check that `laya serve` is running, then press R to reload")
		}
		return dimStyle.Render("waiting for presets…")
	}
	d := m.draftFor(p)
	var b strings.Builder
	b.WriteString(boldStyle.Render(p.Title) + dimStyle.Render("  ·  model "+m.modelLabel()) + "\n")
	b.WriteString(dimStyle.Render(p.Blurb) + "\n\n")

	// state preview
	b.WriteString(titleStyle.Render("State") + dimStyle.Render(" ("+d.stateKey+")") + "\n")
	b.WriteString(truncate(d.stateText(), 280) + "\n")
	if extra := len(d.extraFields()); extra > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("+ %d more field(s)", extra)) + "\n")
	}
	b.WriteString("\n" + m.lengthMeter(d) + "\n\n")

	hasResult := m.result != nil && m.resultOf == p.Name

	switch {
	case m.runErr != "":
		b.WriteString(errStyle.Render(m.runErr) + "\n")
		if hint := paramHint(m.runErrParam); hint != "" {
			b.WriteString(dimStyle.Render(hint) + "\n")
		}
	case m.running:
		b.WriteString(dimStyle.Render("running…") + "\n")
	case hasResult && m.showRaw:
		b.WriteString(titleStyle.Render("Raw response") + "\n" + m.result.PrettyRaw() + "\n")
	case hasResult:
		b.WriteString(m.viewResult(p, width))
	default:
		b.WriteString(titleStyle.Render("Questions") + dimStyle.Render(fmt.Sprintf("  (%d, enter to run)", len(d.questions))) + "\n")
		for _, nq := range d.questions {
			b.WriteString(fmt.Sprintf("  %-18s %s\n", nq.Name, dimStyle.Render(nq.Type)))
		}
	}
	return b.String()
}

func (m Model) viewResult(p api.Preset, width int) string {
	out := renderDecisions(p.Name, m.summaries, width, m.truncated)
	if m.result != nil && m.resultOf == p.Name {
		out += "\n" + m.costLine(width)
	}
	return out
}

// costLine is the post-run readout. It intentionally leads with the numbers
// that explain latency: server ms, ms per question, then throughput.
func (m Model) costLine(width int) string {
	r := m.result
	if r == nil {
		return ""
	}
	q := len(m.resultReq.Questions)
	ms := r.Usage.LatencyMs
	msPerQ := 0.0
	tokPerSec, qPerSec := 0.0, 0.0
	if q > 0 {
		msPerQ = ms / float64(q)
	}
	tokens := r.Usage.InputTokens + r.Usage.OutputTokens
	if ms > 0 {
		tokPerSec = float64(tokens) / (ms / 1000)
		qPerSec = float64(q) / (ms / 1000)
	}
	pred := ""
	if m.predictor != nil {
		rep := lengthguard.Check(r.Family, m.resultReq.State, m.resultReq.Questions)
		p := m.predictor.Predict(r.Family, q, rep.Longest)
		pred = fmt.Sprintf("predicted %.0fms (%s", p.MS, p.Source)
		if p.Source.String() == "fit" {
			pred += fmt.Sprintf(", n=%d", p.Samples)
		}
		pred += ")"
	}
	line1 := fmt.Sprintf("  wall %dms · server %.0fms · %d q · %.0f ms/q",
		r.Wall.Milliseconds(), ms, q, msPerQ)
	line2 := fmt.Sprintf("      %d in / %d out · %s tok/s · %s q/s",
		r.Usage.InputTokens, r.Usage.OutputTokens, compact(tokPerSec), compact(qPerSec))
	if pred != "" {
		line2 += " · " + pred
	}
	return titleStyle.Render("Cost") + dimStyle.Render(truncate(line1, width-4)) + "\n" +
		dimStyle.Render(truncate(line2, width))
}

func compact(v float64) string {
	switch {
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

// renderDecisions is shared by the browse result and stored history entries.
func renderDecisions(preset string, summaries []lens.Summary, width int, truncated bool) string {
	var b strings.Builder

	// harness: the next-action choice gets a banner
	if preset == "harness" {
		for _, s := range summaries {
			if s.Name == "next" {
				b.WriteString(renderBanner(s, width) + "\n\n")
			}
		}
	}

	b.WriteString(titleStyle.Render("Decisions") + "\n")
	nameW, labelW := 0, 0
	for _, s := range summaries {
		if n := len([]rune(s.Name)); n > nameW {
			nameW = n
		}
		if n := len([]rune(shortLabel(s.Label))); n > labelW {
			labelW = n
		}
	}
	for _, s := range summaries {
		b.WriteString(renderSummary(s, nameW, labelW, width) + "\n")
	}
	if truncated {
		b.WriteString("\n" + fg(colAmber, "input_tokens shows the state hit the cap: its end was cut silently") + "\n")
	}
	return b.String()
}

// lengthMeter is the pre-send length guard line.
func (m Model) lengthMeter(d *draft) string {
	family := m.previewFamily(d)
	note := ""
	if m.model == "" || m.model == "auto" {
		switch {
		case m.routeFamily(d) != "":
			note = " (route preview)"
		case m.result == nil || m.resultOf != d.preset:
			note = " (assumed; press f to pin)"
		}
	}
	r := lengthguard.Check(family, d.state, d.questions)
	line := fg(levelColor(r.Level), fmt.Sprintf("~%d / %d tokens", r.Longest, r.Cap)) +
		dimStyle.Render(" longest sequence, "+family+note)
	if r.Level == lengthguard.Over {
		line += "\n" + errStyle.Render("the end of the state would be cut silently")
	}
	if r.HeadLevel != lengthguard.OK {
		line += "\n" + fg(levelColor(r.HeadLevel), fmt.Sprintf("question part ~%d / %d tokens", r.Head, r.HeadCap))
	}
	if r.Longest > 0 {
		line += "\n" + dimStyle.Render("cost follows the longest question: one long question slows the whole request")
	}
	return line
}

// shortLabel makes a level label fit a column: for labels like
// "serious: unsafe advice or abuse" the part before the colon is the name.
func shortLabel(label string) string {
	if i := strings.Index(label, ":"); i > 0 {
		label = label[:i]
	}
	return truncate(label, 16)
}

// piece is one optional part of a Decision Lens row.
type piece struct {
	plain  string // visible text, used to measure width
	styled string
	prio   int // lower is kept first when the row is too wide
}

// renderSummary is one Decision Lens row, fitted to width. Parts are dropped
// from least to most important (server confidence, then runner-up/expected,
// then certainty) so a row never wraps in a narrow pane.
func renderSummary(s lens.Summary, nameW, labelW, width int) string {
	col := probColor(s.Prob)
	name := fmt.Sprintf("  %-*s", nameW, truncate(s.Name, nameW))
	headTxt := fmt.Sprintf("%-*s %3.0f%%", labelW, shortLabel(s.Label), 100*s.Prob)
	head := lipgloss.NewStyle().Foreground(col).Bold(true).Render(headTxt)

	var graphic, extra string
	switch s.Type {
	case api.TypeNoul:
		graphic = bar(s.Prob, 10)
	case api.TypeScore:
		graphic = spark(s.Levels)
		if s.Expected != nil {
			extra = fmt.Sprintf("E=%.2f", *s.Expected)
		}
	default:
		graphic = bar(s.Prob, 10)
		if s.RunnerUp.Label != "" {
			extra = fmt.Sprintf("then %s %.0f%%", truncate(s.RunnerUp.Label, 12), 100*s.RunnerUp.Prob)
		}
	}

	var contested, contestedStyled string
	if s.Contested {
		contested = "CONTESTED"
		contestedStyled = lipgloss.NewStyle().Foreground(colAmber).Bold(true).Render(contested)
	}
	sat, satStyled := "", ""
	if s.Saturated {
		sat, satStyled = "sat", dimStyle.Render("sat")
	}

	// display order; prio decides what survives a narrow pane (lower is kept first)
	cert := fmt.Sprintf("cert %.2f", s.Certainty)
	srv := fmt.Sprintf("srv %.2f", s.ServerConfidence)
	parts := []piece{
		{graphic, graphic, 2},
		{extra, dimStyle.Render(extra), 5},
		{cert, dimStyle.Render(cert), 3},
		{srv, dimStyle.Render(srv), 6},
		{sat, satStyled, 4},
		{contested, contestedStyled, 1},
	}

	used := len([]rune(name)) + 2 + len([]rune(headTxt))
	keep := make([]bool, len(parts))
	for prio := 1; prio <= 6; prio++ {
		for i, p := range parts {
			if p.prio != prio || p.plain == "" {
				continue
			}
			if need := 2 + len([]rune(p.plain)); used+need <= width {
				keep[i] = true
				used += need
			}
		}
	}

	var b strings.Builder
	b.WriteString(name + "  " + head)
	for i, p := range parts {
		if keep[i] {
			b.WriteString("  " + p.styled)
		}
	}
	return b.String()
}

// renderBanner is the large decision display for the harness preset. A real
// big-letter font is a later upgrade; spaced capitals carry the idea for now.
func renderBanner(s lens.Summary, width int) string {
	col := probColor(s.Prob)
	title := strings.ToUpper(strings.ReplaceAll(s.Label, "_", " "))
	title = strings.Join(strings.Split(title, ""), " ")
	sub := fmt.Sprintf("%.0f%%", 100*s.Prob)
	if s.RunnerUp.Label != "" {
		sub += fmt.Sprintf(" · runner-up %s %.0f%%", s.RunnerUp.Label, 100*s.RunnerUp.Prob)
	}
	if s.Contested {
		col = colAmber
		title = "? " + title
		sub = "CONTESTED · " + sub
	}
	w := width - 6
	if w < 20 {
		w = 20
	}
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).BorderForeground(col).
		Foreground(col).Bold(true).
		Padding(1, 2).Align(lipgloss.Center).Width(w).
		Render(title + "\n" + sub)
}

// telemetryBar is the persistent bottom bar. Items are ordered by importance
// because a narrow terminal truncates from the right.
func (m Model) telemetryBar(width int) string {
	var parts []string
	switch m.status {
	case statusConnecting:
		parts = append(parts, "connecting…")
	case statusWarming:
		parts = append(parts, "warming up…")
	case statusReady:
		parts = append(parts, "ready")
	case statusError:
		parts = append(parts, "error: "+m.statusMsg)
	}
	if m.health != nil {
		parts = append(parts, m.health.Device)
	}
	if m.result != nil {
		parts = append(parts, m.result.Family)
		parts = append(parts, m.result.Route)
		last := fmt.Sprintf("last %.0fms (wall %dms)", m.result.Usage.LatencyMs, m.result.Wall.Milliseconds())
		if m.lastCold {
			last += " cold"
		}
		parts = append(parts, last)
	}
	for _, kind := range []string{"single", "preset"} {
		if med, n, ok := m.tele.Median(kind); ok {
			parts = append(parts, fmt.Sprintf("%s p50 %.0fms (n=%d)", kind, med, n))
		}
	}
	// Style.Width includes the horizontal padding, so the text gets width-2.
	return barStyle.Width(width).Render(truncate(strings.Join(parts, " │ "), width-2))
}

// bar draws p in [0,1] as a filled bar of the given width.
func bar(p float64, width int) string {
	n := int(math.Round(math.Max(0, math.Min(1, p)) * float64(width)))
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

// spark draws a distribution as one block per level, in level order.
func spark(levels []lens.Level) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	out := make([]rune, 0, len(levels))
	for _, l := range levels {
		i := int(l.Prob * float64(len(blocks)))
		if i >= len(blocks) {
			i = len(blocks) - 1
		}
		if i < 0 {
			i = 0
		}
		out = append(out, blocks[i])
	}
	return string(out)
}

// truncate shortens s to at most n runes, adding an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// clipLines keeps at most max lines so a long pane cannot push the bottom bar
// off the screen.
func clipLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n")
}
