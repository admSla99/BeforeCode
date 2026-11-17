// PROTOTYPE, throw away. Issue #16: three structurally different layouts of
// the TUI plan list screen, on fake in-memory data, to settle the layout
// with the maintainer before the tested version is written.
//
//	go run ./internal/tui/prototype/planlist              # interactive, F1/F2/F3 switch variants
//	go run ./internal/tui/prototype/planlist --variant B
//	go run ./internal/tui/prototype/planlist --dump       # print every variant at 80x24
//
// A: table   B: grouped by state   C: list with preview pane
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type plan struct {
	short, state, title, author, activity, intent string
}

var fake = []plan{
	{"bfc-5v7x", "in-review", "Payments via Stripe", "jana@example.com", "2026-11-17 09:42", "Cards fail at checkout for 3% of EU customers."},
	{"bfc-k2m9", "approved", "Rate-limit the public API", "peter@example.com", "2026-11-17 08:10", "One client floods /search and slows everyone down."},
	{"bfc-q8r1", "draft", "Export plans to HTML", "ada@example.com", "2026-11-16 21:30", "Clients cannot open the plans ref without bfc."},
	{"bfc-a7c3", "in-review", "Move the index to SQLite FTS5", "ada@example.com", "2026-11-16 17:05", "bfc list gets slow above 500 plans."},
	{"bfc-z0e4", "draft", "Dark mode for the TUI", "ben@example.com", "2026-11-15 11:20", "Some terminals render the default palette badly."},
	{"bfc-m3n6", "closed", "Replace Cobra with urfave/cli", "ben@example.com", "2026-11-14 09:00", "Smaller binary. Closed: not worth the churn."},
	{"bfc-t5y2", "approved", "Retry pushes with backoff", "jana@example.com", "2026-11-13 15:45", "Pushes from CI fail when two jobs race."},
}

var stateFilters = []string{"", "draft", "in-review", "approved", "closed"}

type model struct {
	variant       string
	plans         []plan
	cursor        int
	filter        string
	filtering     bool
	stateFilter   int
	width, height int
	mode          string // "", "empty", "uninit"
}

func (m model) Init() tea.Cmd { return nil }

func (m model) visible() []plan {
	if m.mode != "" {
		return nil
	}
	var out []plan
	for _, p := range m.plans {
		if sf := stateFilters[m.stateFilter]; sf != "" && p.state != sf {
			continue
		}
		if m.filter != "" && !strings.Contains(strings.ToLower(p.title+" "+p.short+" "+p.author), strings.ToLower(m.filter)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		k := msg.String()
		if m.filtering {
			switch k {
			case "enter", "esc":
				m.filtering = false
				if k == "esc" {
					m.filter = ""
				}
			case "backspace":
				if m.filter != "" {
					m.filter = m.filter[:len(m.filter)-1]
				}
			default:
				if len(msg.Text) > 0 {
					m.filter += msg.Text
				}
			}
			m.cursor = 0
			return m, nil
		}
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.cursor = max(m.cursor-1, 0)
		case "down", "j":
			m.cursor = min(m.cursor+1, max(len(m.visible())-1, 0))
		case "/":
			m.filtering = true
		case "s":
			m.stateFilter = (m.stateFilter + 1) % len(stateFilters)
			m.cursor = 0
		case "f1":
			m.variant = "A"
		case "f2":
			m.variant = "B"
		case "f3":
			m.variant = "C"
		case "e":
			m.mode = map[string]string{"": "empty", "empty": "uninit", "uninit": ""}[m.mode]
		}
	}
	return m, nil
}

var (
	title    = lipgloss.NewStyle().Bold(true)
	dim      = lipgloss.NewStyle().Faint(true)
	selected = lipgloss.NewStyle().Reverse(true)
	stateCol = map[string]lipgloss.Style{
		"draft":     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"in-review": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"approved":  lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		"closed":    lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true),
	}
)

func pad(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

func (m model) header() string {
	sf := stateFilters[m.stateFilter]
	if sf == "" {
		sf = "all"
	}
	f := m.filter
	if m.filtering {
		f += "▌"
	}
	if f == "" {
		f = dim.Render("(/ to search)")
	}
	return title.Render("BeforeCode · plans") + "   state: " + sf + "   filter: " + f
}

func (m model) help() string {
	return dim.Render("↑↓/jk move  / search  s state  n new  enter open  q quit  F1-3 variant")
}

func (m model) special() (string, bool) {
	switch m.mode {
	case "empty":
		return "\n  No plans yet.\n\n  Press n to write the first intent,\n  or run bfc pull if your team already has plans.\n", true
	case "uninit":
		return "\n  BeforeCode is not set up in this repository.\n\n  bfc init        start using BeforeCode here\n  bfc bootstrap   get the plans of a team that already uses it\n", true
	}
	if len(m.visible()) == 0 {
		return "\n  No plans match. Esc clears the search, s changes the state filter.\n", true
	}
	return "", false
}

// Variant A: one full-width table, one row per plan.
func (m model) viewA() string {
	w := m.width
	var b strings.Builder
	b.WriteString(m.header() + "\n\n")
	if s, ok := m.special(); ok {
		return b.String() + s
	}
	titleW := max(w-9-10-8-11-4, 10)
	b.WriteString(dim.Render(pad("ID", 9)+" "+pad("STATE", 10)+" "+pad("TITLE", titleW)+" "+pad("AUTHOR", 8)+" "+"ACTIVITY") + "\n")
	for i, p := range m.visible() {
		who, _, _ := strings.Cut(p.author, "@")
		row := pad(p.short, 9) + " " + stateCol[p.state].Render(pad(p.state, 10)) + " " + pad(p.title, titleW) + " " + pad(who, 8) + " " + p.activity[5:]
		if i == m.cursor {
			row = selected.Render(pad(p.short, 9) + " " + pad(p.state, 10) + " " + pad(p.title, titleW) + " " + pad(who, 8) + " " + p.activity[5:])
		}
		b.WriteString(row + "\n")
	}
	return b.String()
}

// Variant B: grouped by state, two lines per plan, counts per group.
func (m model) viewB() string {
	var b strings.Builder
	b.WriteString(m.header() + "\n")
	if s, ok := m.special(); ok {
		return b.String() + s
	}
	vis := m.visible()
	i := 0
	for _, st := range []string{"in-review", "approved", "draft", "closed"} {
		var group []plan
		for _, p := range vis {
			if p.state == st {
				group = append(group, p)
			}
		}
		if len(group) == 0 {
			continue
		}
		b.WriteString("\n" + stateCol[st].Bold(true).Render(fmt.Sprintf("%s (%d)", strings.ToUpper(st), len(group))) + "\n")
		for _, p := range group {
			line1 := "  " + p.title
			line2 := "  " + dim.Render(p.short+" · "+p.author+" · "+p.activity)
			if visIndex(vis, p) == m.cursor {
				line1 = selected.Render("▶ " + p.title)
			}
			b.WriteString(line1 + "\n" + line2 + "\n")
			i++
		}
	}
	return b.String()
}

func visIndex(vis []plan, p plan) int {
	for i, q := range vis {
		if q.short == p.short {
			return i
		}
	}
	return -1
}

// Variant C: narrow list on the left, preview of the selected plan on the
// right.
func (m model) viewC() string {
	head := m.header() + "\n\n"
	if s, ok := m.special(); ok {
		return head + s
	}
	leftW := m.width/2 - 1
	var left strings.Builder
	vis := m.visible()
	for i, p := range vis {
		row := pad(p.short, 9) + " " + pad(p.title, leftW-10)
		if i == m.cursor {
			row = selected.Render(row)
		} else {
			row = stateCol[p.state].Render(pad(p.short, 9)) + " " + pad(p.title, leftW-10)
		}
		left.WriteString(row + "\n")
	}
	p := vis[min(m.cursor, len(vis)-1)]
	right := title.Render(p.title) + "\n" +
		dim.Render(p.short) + "\n\n" +
		"State     " + stateCol[p.state].Render(p.state) + "\n" +
		"Author    " + p.author + "\n" +
		"Activity  " + p.activity + "\n\n" +
		dim.Render("Problem") + "\n" + lipgloss.NewStyle().Width(m.width-leftW-4).Render(p.intent)
	box := lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).PaddingLeft(1)
	return head + lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(leftW).Render(left.String()), box.Render(right))
}

func (m model) render() string {
	var body string
	switch m.variant {
	case "B":
		body = m.viewB()
	case "C":
		body = m.viewC()
	default:
		body = m.viewA()
	}
	// Pin the help line to the bottom row.
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for len(lines) < m.height-1 {
		lines = append(lines, "")
	}
	lines = append(lines[:min(len(lines), m.height-1)], m.help())
	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func main() {
	variant := flag.String("variant", "A", "A, B or C")
	dump := flag.Bool("dump", false, "print every variant at 80x24 and exit")
	flag.Parse()
	if *dump {
		for _, v := range []string{"A", "B", "C"} {
			for _, mode := range []string{"", "empty"} {
				m := model{variant: v, plans: fake, width: 80, height: 24, mode: mode, cursor: 1}
				fmt.Printf("===== variant %s %s =====\n%s\n", v, mode, ansi.ReplaceAllString(m.render(), ""))
			}
		}
		m := model{variant: "A", plans: fake, width: 80, height: 24, filter: "pay", stateFilter: 2}
		fmt.Printf("===== variant A, state in-review, search \"pay\" =====\n%s\n", ansi.ReplaceAllString(m.render(), ""))
		return
	}
	if _, err := tea.NewProgram(model{variant: *variant, plans: fake}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
