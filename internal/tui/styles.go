package tui

import (
	"github.com/charmbracelet/lipgloss"

	"playa/internal/lengthguard"
)

var (
	colGreen  = lipgloss.Color("42")
	colAmber  = lipgloss.Color("214")
	colRed    = lipgloss.Color("196")
	colDim    = lipgloss.Color("245")
	colAccent = lipgloss.Color("63")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	dimStyle      = lipgloss.NewStyle().Foreground(colDim)
	boldStyle     = lipgloss.NewStyle().Bold(true)
	errStyle      = lipgloss.NewStyle().Foreground(colRed)
	barStyle      = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	tabActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(colDim)
	toastStyle       = lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("252")).
				Padding(0, 1)
	toastErrStyle = toastStyle.Foreground(colRed)
)

// probColor colors by the probability of the winning class, not by the
// server's confidence (whose meaning differs per question type).
func probColor(p float64) lipgloss.Color {
	switch {
	case p >= 0.85:
		return colGreen
	case p >= 0.60:
		return colAmber
	}
	return colRed
}

func levelColor(l lengthguard.Level) lipgloss.Color {
	switch l {
	case lengthguard.Over:
		return colRed
	case lengthguard.Warn:
		return colAmber
	}
	return colGreen
}

func fg(c lipgloss.Color, s string) string {
	return lipgloss.NewStyle().Foreground(c).Render(s)
}
