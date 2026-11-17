// PROTOTYPE, throw away. Issue #17: three structurally different layouts of
// the TUI plan detail screen, on fake in-memory data, to settle the layout
// with the maintainer before the tested version is written.
//
//	go run ./internal/tui/prototype/plandetail              # F1/F2/F3 switch variants
//	go run ./internal/tui/prototype/plandetail --dump       # every variant at 80x24
//
// A: header block + tabs   B: one-line header, status bar   C: side panel
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const intentMD = `## Problem

Cards fail at checkout for 3% of EU customers because the old provider
does not support 3-D Secure 2.

## Who it is for

Shoppers paying by card in the EU, and the support team handling the
complaints.

## How we know it worked

Card failures at checkout drop below 0.5% within two weeks.

## Out of scope

Refunds, PayPal, invoices.
`

const planMD = `## Context

The checkout calls ` + "`payments.Charge`" + ` which wraps the old provider.

## Approach

Add a Stripe client behind the existing ` + "`payments.Provider`" + ` interface
and switch EU traffic with a feature flag.

## Affected areas

- ` + "`payments/`" + `
- ` + "`checkout/handler.go`" + `

## Out of scope

Refunds.

## Tasks

- Add the Stripe client.
- Wire the feature flag.

## Acceptance criteria

- AC-1: With an invalid card the API returns 402 and the order stays in
  state awaiting-payment.
- AC-2: A successful payment sends exactly one confirmation email, even
  when the webhook is delivered twice.
`

type model struct {
	variant       string
	doc           int // 0 intent.md, 1 plan.md
	scroll        int
	sync          string
	width, height int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.doc, m.scroll = 1-m.doc, 0
		case "down", "j":
			m.scroll++
		case "up", "k":
			m.scroll = max(m.scroll-1, 0)
		case "y":
			m.sync = map[string]string{"synced": "syncing", "syncing": "offline", "offline": "conflict", "conflict": "synced"}[m.sync]
		case "f1":
			m.variant = "A"
		case "f2":
			m.variant = "B"
		case "f3":
			m.variant = "C"
		}
	}
	return m, nil
}

var (
	bold   = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Faint(true)
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	tabOn  = lipgloss.NewStyle().Bold(true).Underline(true)
)

func syncBadge(s string) string {
	switch s {
	case "syncing":
		return yellow.Render("⟳ syncing")
	case "offline":
		return dim.Render("○ offline · 1 change waiting")
	case "conflict":
		return lipgloss.NewStyle().Foreground(lipgloss.Red).Render("✗ conflict in plan.md")
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Green).Render("● synced 12s ago")
}

func render(md string, width int) []string {
	r, _ := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
	out, _ := r.Render(md)
	return strings.Split(strings.Trim(out, "\n"), "\n")
}

func (m model) docLines(width int) []string {
	if m.doc == 0 {
		return render(intentMD, width)
	}
	return render(planMD, width)
}

func window(lines []string, scroll, h int) []string {
	scroll = min(scroll, max(len(lines)-h, 0))
	lines = lines[scroll:min(len(lines), scroll+h)]
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines
}

func (m model) tabs() string {
	names := []string{"intent.md", "plan.md"}
	for i := range names {
		if i == m.doc {
			names[i] = tabOn.Render(names[i])
		} else {
			names[i] = dim.Render(names[i])
		}
	}
	return names[0] + "   " + names[1] + "   " + dim.Render("(tab)")
}

const helpLine = "tab doc  e edit  s submit  a approve  r changes  ↑↓ scroll  esc back"

// Variant A: a four-line header block, a tab bar, the document, sync status
// on the help row.
func (m model) viewA() []string {
	head := []string{
		bold.Render("bfc-5v7x  Payments via Stripe"),
		"State " + yellow.Render("in-review") + "  ·  Author jana@example.com  ·  Approvals 1/2 (peter)",
		m.tabs(),
		strings.Repeat("─", m.width),
	}
	foot := []string{dim.Render(helpLine) + "  " + syncBadge(m.sync)}
	body := window(m.docLines(m.width-2), m.scroll, m.height-len(head)-len(foot))
	return append(append(head, body...), foot...)
}

// Variant B: one header line, the document fills the screen, a status bar
// with state, approvals and sync above the help line.
func (m model) viewB() []string {
	head := []string{bold.Render("bfc-5v7x  Payments via Stripe") + "   " + m.tabs()}
	bar := lipgloss.NewStyle().Reverse(true).Width(m.width).Render(
		fmt.Sprintf(" in-review · 1/2 approvals (peter) · jana@example.com     %s", ansi.Strip(syncBadge(m.sync))))
	foot := []string{bar, dim.Render(helpLine)}
	body := window(m.docLines(m.width-2), m.scroll, m.height-len(head)-len(foot))
	return append(append(head, body...), foot...)
}

// Variant C: a narrow side panel with the plan's facts and the actions that
// are possible now; the document on the right.
func (m model) viewC() []string {
	sideW := 24
	side := []string{
		bold.Render("bfc-5v7x"),
		"Payments via Stripe",
		"",
		dim.Render("State"), yellow.Render("in-review"),
		dim.Render("Author"), "jana@example.com",
		dim.Render("Approvals"), "1 of 2 · peter",
		"",
		dim.Render("Actions"), "e  edit " + []string{"intent", "plan"}[m.doc], "a  approve", "r  request changes",
		"",
		dim.Render("Sync"), syncBadge(m.sync),
	}
	h := m.height - 1
	docW := m.width - sideW - 3
	body := append([]string{m.tabs(), ""}, window(m.docLines(docW), m.scroll, h-2)...)
	var lines []string
	for i := range h {
		l := ""
		if i < len(side) {
			l = side[i]
		}
		lines = append(lines, lipgloss.NewStyle().Width(sideW).MaxWidth(sideW).Render(l)+" │ "+ansi.Truncate(body[i], docW, "…"))
	}
	return append(lines, dim.Render("tab doc  ↑↓ scroll  esc back  q quit"))
}

func (m model) screen() string {
	var lines []string
	switch m.variant {
	case "B":
		lines = m.viewB()
	case "C":
		lines = m.viewC()
	default:
		lines = m.viewA()
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	return v
}

func main() {
	dump := flag.Bool("dump", false, "print every variant at 80x24 and exit")
	flag.Parse()
	if *dump {
		for _, v := range []string{"A", "B", "C"} {
			m := model{variant: v, doc: 1, sync: "synced", width: 80, height: 24}
			fmt.Printf("===== variant %s =====\n%s\n", v, ansi.Strip(m.screen()))
		}
		return
	}
	if _, err := tea.NewProgram(model{variant: "A", sync: "synced"}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
