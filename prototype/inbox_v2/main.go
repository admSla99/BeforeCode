// PROTOTYPE for issue #113, throwaway, never merged: the Inbox v2 of #82
// with fake data, in two variants for a plan in progress.
//
//	go run ./prototype/inbox_v2            # interactive: v variant, e empty, x hide hint, q quit
//	go run ./prototype/inbox_v2 -print     # print the variants as 80x24 frames
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const width, height = 80, 24

// Fake data. I am Jana.
type task struct {
	id, title, state, holder string // state: completed, claimed, paused, open
	mine                     bool
	handoff                  string // first line of the latest Handoff
}

type plan struct {
	short, title, label, state string
	tasks                      []task
}

var (
	readyForYou = []string{
		"Finding   bfc-9k2m  PR #41: AC-3 is not met",
		"Review    bfc-7q1z  Webhook retries (needs 1 more Approval)",
		"Question  bfc-5v7x  Peter: Does the webhook retry? (plan.md#AC-2)",
	}
	myWork = []plan{
		{short: "bfc-5v7x", title: "Payments via Stripe", label: "payments", state: "in-progress", tasks: []task{
			{id: "T-1", title: "Add the Stripe client", state: "completed", holder: "Jana"},
			{id: "T-2", title: "Wire the feature flag", state: "claimed", holder: "Jana", mine: true},
			{id: "T-3", title: "Remove the old provider", state: "claimed", holder: "Peter (claude-code)"},
			{id: "T-4", title: "Migrate saved cards", state: "open"},
			{id: "T-5", title: "Update the runbook", state: "open"},
		}},
	}
	forAgent = []string{
		"bfc-5v7x  T-4  Migrate saved cards",
		"bfc-5v7x  T-5  Update the runbook",
		"bfc-2c8d  T-1  Rate-limit the export endpoint",
	}
	paused = []task{
		{id: "T-2", title: "Split the email worker", state: "paused", holder: "Ben", handoff: "Queue done; the retry policy is next."},
	}
	pausedPlan = "bfc-3h6w"
)

var (
	rule  = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	faint = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	bold  = lipgloss.NewStyle().Bold(true)
	you   = lipgloss.NewStyle().Background(lipgloss.Yellow).Foreground(lipgloss.Black)
	short = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	warm  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	box   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Yellow).Padding(0, 1).Width(width - 2)
)

func section(name, count string) string {
	head := bold.Render(name) + "  " + faint.Render(count) + " "
	return head + rule.Render(strings.Repeat("─", max(0, width-ansi.StringWidth(head))))
}

func icon(state string) string {
	switch state {
	case "completed":
		return "✓"
	case "claimed":
		return warm.Render("◐")
	case "paused":
		return "‖"
	}
	return "○"
}

func taskLine(t task) string {
	line := icon(t.state) + " " + t.title + "  " + warm.Render(t.state) + "  " + short.Render(t.id)
	if t.holder != "" && t.state != "completed" {
		if t.mine {
			line += "  " + you.Render(" you ")
		} else {
			line += "  " + faint.Render(t.holder)
		}
	}
	return line
}

func planLine(p plan, extra string) string {
	return "  " + bold.Render(p.title) + "  " + short.Render(p.short) + "  " + p.label + "  " + warm.Render(p.state) + faint.Render(extra)
}

// flat: every Task of the plan, one line each, in plan.md order.
func flat(p plan) []string {
	out := []string{planLine(p, "  1/5 tasks")}
	for _, t := range p.tasks {
		out = append(out, "    "+taskLine(t))
	}
	return out
}

// stages: Tasks have no dependencies, so the stages come from the state:
// Done (collapsed), Now (the claimed Tasks, expanded), Next (the open
// Tasks on one line, in plan.md order).
func stages(p plan) []string {
	var done, now, next []task
	for _, t := range p.tasks {
		switch t.state {
		case "completed":
			done = append(done, t)
		case "open":
			next = append(next, t)
		default:
			now = append(now, t)
		}
	}
	titles := func(ts []task) string {
		s := make([]string, len(ts))
		for i, t := range ts {
			s[i] = t.title
		}
		return strings.Join(s, " → ")
	}
	out := []string{planLine(p, "")}
	out = append(out, "  ├── "+faint.Render(fmt.Sprintf("Done (%d)  ", len(done))+titles(done)))
	out = append(out, "  ├── ▾ Now")
	for i, t := range now {
		branch := "├──"
		if i == len(now)-1 {
			branch = "└──"
		}
		out = append(out, "  │   "+branch+" "+taskLine(t))
	}
	out = append(out, "  └── "+faint.Render(fmt.Sprintf("Next (%d)  ", len(next))+titles(next)))
	return out
}

type model struct {
	variant  int // 0 flat, 1 stages
	hideHint bool
	quiet    bool // nothing needs me, nothing for an agent, nothing paused
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "v":
			m.variant = 1 - m.variant
		case "x":
			m.hideHint = true
		case "e":
			m.quiet = !m.quiet
		}
	}
	return m, nil
}

func (m model) View() tea.View { return tea.NewView(m.render()) }

func (m model) render() string {
	ready, agent, pause := readyForYou, forAgent, paused
	summary := bold.Render("3 things need you.") + "  " + faint.Render("1 Finding, 1 review, 1 Question")
	if m.quiet {
		ready, agent, pause = nil, nil, nil
		summary = bold.Render("You're clear.") + "  " + faint.Render("Nothing needs you right now.")
	}
	count := func(n int, none string) string {
		if n == 0 {
			return none
		}
		return fmt.Sprint(n)
	}
	var l []string
	l = append(l, strings.Split(box.Render(summary), "\n")...)
	l = append(l, section("READY FOR YOU", count(len(ready), "nothing ready")))
	for _, r := range ready {
		l = append(l, "  "+r)
	}
	l = append(l, "", section("YOUR WORK", "by plan"))
	for _, p := range myWork {
		if m.variant == 0 {
			l = append(l, flat(p)...)
		} else {
			l = append(l, stages(p)...)
		}
	}
	l = append(l, "", section("READY FOR AN AGENT", count(len(agent), "nothing ready")))
	for _, a := range agent {
		l = append(l, "  "+a)
	}
	l = append(l, "", section("PAUSED", count(len(pause), "nothing paused")))
	for _, t := range pause {
		l = append(l, "  "+short.Render(pausedPlan)+"  "+t.id+"  "+t.title+faint.Render("  by "+t.holder+" · "+t.handoff))
	}
	for len(l) < height-1 {
		l = append(l, "")
	}
	l = l[:height-1]
	foot := "enter open • c claim • v variant • e empty • q quit"
	if !m.hideHint {
		foot = faint.Render("Working alone · ") + you.Render(" x ") + " hide • " + foot
	}
	l = append(l, foot)
	for i := range l {
		l[i] = ansi.Truncate(l[i], width, "…")
	}
	return strings.Join(l, "\n")
}

func main() {
	print := flag.Bool("print", false, "print the variants as plain 80x24 frames")
	flag.Parse()
	if *print {
		for i, name := range []string{"A: flat list of Tasks", "B: stages tree (Done / Now / Next)"} {
			fmt.Printf("=== variant %s ===\n%s\n\n", name, ansi.Strip(model{variant: i, hideHint: true}.render()))
		}
		fmt.Printf("=== variant B, quiet: empty sections on one line, hint shown ===\n%s\n\n", ansi.Strip(model{variant: 1, quiet: true}.render()))
		return
	}
	if _, err := tea.NewProgram(model{}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
