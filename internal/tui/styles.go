package tui

import (
	"fmt"
	"strings"

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
	styleWarn     = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	styleOK       = lipgloss.NewStyle().Foreground(lipgloss.Green)
	styleAccent   = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
)

// stateStyle colors a Plan state.
func stateStyle(s core.State) lipgloss.Style {
	switch s {
	case core.StateInReview:
		return lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	case core.StateApproved, core.StateInProgress:
		return lipgloss.NewStyle().Foreground(lipgloss.Green)
	case core.StateClosed, core.StateDone, core.StateInvalid:
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

// wrap wraps text to w cells.
func wrap(text string, w int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(strings.TrimSpace(text), max(w, 1), ""), "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// plural is n with noun, which gets an s unless n is 1: "1 Question",
// "2 open Questions".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func tail(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func pad(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
