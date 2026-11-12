package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"playa/internal/launch"
)

func (m Model) viewLaunch() string {
	e := m.launch
	if e == nil {
		return "Launchpad unavailable"
	}
	w, h := m.width, m.height
	if w < 60 {
		w = 60
	}

	header := titleStyle.Render("Launchpad") + dimStyle.Render("  ·  "+e.targetURL)
	if e.status != "" {
		header += dimStyle.Render("  ·  " + truncate(oneLine(e.status), max(8, w-42)))
	}
	parts := []string{header}
	parts = append(parts, m.launchStateLine(e, w))
	for i := 0; i < int(launchFieldCount); i++ {
		parts = append(parts, m.launchFieldLine(e, i, w))
	}

	if bodyH := h - len(parts); bodyH > 0 {
		body := clipLines(m.launchBody(e, w, bodyH), bodyH)
		parts = append(parts, lipgloss.NewStyle().Height(bodyH).Render(body))
	}
	out := strings.Join(parts, "\n")
	if h <= 0 {
		return ""
	}
	return clipLines(out, h)
}

func (m Model) launchStateLine(e *launchSession, w int) string {
	state := "no spawned process"
	if m.launcher != nil {
		switch {
		case m.launcher.Running():
			state = fmt.Sprintf("spawned pid %d · running", m.launcher.PID())
		case m.launcher.State() == launch.Exited:
			state = "spawned process exited"
			if err := m.launcher.Err(); err != nil {
				state += ": " + oneLine(err.Error())
			}
		default:
			state = "spawned process stopped"
		}
	}
	server := "server " + e.targetURL
	if m.health != nil {
		server = fmt.Sprintf("server %s · %s", e.targetURL, m.health.Device)
	}
	return dimStyle.Render(truncate(state+" · "+server, w))
}

func (m Model) launchFieldLine(e *launchSession, i, w int) string {
	marker, style := "  ", dimStyle
	if e.selected == i {
		marker, style = "▸ ", selectedStyle
	}
	label := fmt.Sprintf("%-10s", launchLabels[i])
	if e.editing && e.selected == i {
		return style.Render(marker+label+" ") + e.input.View()
	}
	value := e.fields[i]
	if value == "" {
		value = "—"
		style = dimStyle
	}
	return style.Render(marker+label+" ") + truncate(value, max(8, w-14))
}

func (m Model) launchBody(e *launchSession, w, bodyH int) string {
	var b strings.Builder
	if e.status != "" {
		b.WriteString(boldStyle.Render(truncate(oneLine(e.status), w)) + "\n")
	}
	if m.launcher == nil {
		b.WriteString(dimStyle.Render("s spawns `laya serve` as a child process.") + "\n")
		b.WriteString(dimStyle.Render("a attaches to an existing server and never stops it.") + "\n")
		return b.String()
	}
	logs := strings.TrimRight(m.launcher.Logs(), "\n")
	if logs == "" {
		b.WriteString(dimStyle.Render("no output yet") + "\n")
		return b.String()
	}
	b.WriteString(titleStyle.Render("Log") + "\n")
	lines := strings.Split(logs, "\n")
	keep := max(1, bodyH-2)
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	for _, line := range lines {
		b.WriteString(truncate(line, w) + "\n")
	}
	return b.String()
}
