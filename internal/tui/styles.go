package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/admSla99/BeforeCode/internal/core"
)

// The styles of all screens live here, so that screens look alike and a
// theme can later change them in one place. Use the 16 basic ANSI colors.
var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleTab      = lipgloss.NewStyle().Bold(true).Underline(true)
)

// stateStyle colors a Plan state.
func stateStyle(s core.State) lipgloss.Style {
	switch s {
	case core.StateInReview:
		return lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	case core.StateApproved:
		return lipgloss.NewStyle().Foreground(lipgloss.Green)
	case core.StateClosed, core.StateInvalid:
		return lipgloss.NewStyle().Foreground(lipgloss.Red)
	default:
		return lipgloss.NewStyle()
	}
}

// truncate cuts a styled line to width cells, ending it with "…".
func truncate(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
