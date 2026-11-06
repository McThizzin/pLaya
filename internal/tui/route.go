package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"playa/internal/langdetect"
)

const (
	routeDebounce = 350 * time.Millisecond
	routeTimeout  = 3 * time.Second
)

type routeTickMsg struct {
	seq  int
	text string
}

type routeMsg struct {
	seq     int
	text    string
	preview langdetect.Preview
	err     error
}

// queueRoute starts a debounced detect-lang preview for the state text. Every
// call gets a new sequence number, so a result for older text is ignored.
func (m Model) queueRoute(d *draft, text string) (Model, tea.Cmd) {
	m.routeSeq++
	m.routePreset = d.preset
	m.routeText = text
	m.routePreview = langdetect.Preview{}
	m.routeErr = ""
	m.routePending = false
	if m.opts.Detect == nil || strings.TrimSpace(text) == "" {
		return m, nil
	}
	m.routePending = true
	seq := m.routeSeq
	return m, tea.Tick(routeDebounce, func(time.Time) tea.Msg {
		return routeTickMsg{seq: seq, text: text}
	})
}

func (m Model) routeDetectCmd(text string, seq int) tea.Cmd {
	detect := m.opts.Detect
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
		defer cancel()
		p, err := detect(ctx, text)
		return routeMsg{seq: seq, text: text, preview: p, err: err}
	}
}

// routeLine is the one-line preview shown under the length meter.
func (m Model) routeLine() string {
	if m.opts.Detect == nil {
		return ""
	}
	switch {
	case m.routePending:
		return dimStyle.Render("route: detecting…")
	case m.routeErr != "":
		return dimStyle.Render("route: ") + errStyle.Render(truncate(oneLine(m.routeErr), 120))
	case m.routePreview.Route != "":
		line := "route: " + m.routePreview.Route
		if m.routePreview.Reason != "" {
			line += " · " + m.routePreview.Reason
		}
		if m.routePreview.Script != "" {
			line += " · " + m.routePreview.Script
		}
		return dimStyle.Render(line)
	}
	return ""
}

// routeFamily is the family suggested by the preview, if it belongs to d.
func (m Model) routeFamily(d *draft) string {
	if d == nil || m.routePreset != d.preset {
		return ""
	}
	return m.routePreview.Family()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// paramHint turns a server error param such as "questions.urgency.criteria"
// into an actionable line.
func paramHint(param string) string {
	if param == "" {
		return ""
	}
	parts := strings.Split(param, ".")
	switch {
	case len(parts) >= 2 && parts[0] == "questions":
		return fmt.Sprintf("server error at question %q — ctrl+q jumps to it", parts[1])
	case len(parts) >= 1 && parts[0] == "state":
		return "server error at the state — ctrl+e opens the State Canvas"
	}
	return "server error at " + param
}

// questionParam returns the question name from a server error param.
func questionParam(param string) string {
	parts := strings.Split(param, ".")
	if len(parts) >= 2 && parts[0] == "questions" {
		return parts[1]
	}
	return ""
}
