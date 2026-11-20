// PROTOTYPE, throw away. Issue #62: three structurally different layouts of
// the TUI Grill screen at 80x24, driven by a fake agent that streams its
// text, to settle the layout and the feel of a turn with the maintainer
// before the tested version (#66) is written. The state model itself is in
// grill-state.html next to this file.
//
//	go run ./internal/tui/prototype/grill              # F1/F2/F3 switch variants
//	go run ./internal/tui/prototype/grill --dump       # every variant in every state, as text
//
// A: chat transcript   B: Intent beside the chat   C: one Question at a time
//
// Keys: Enter start / send the answer, Esc cancel the turn (or leave when
// idle), f finish the Grill, w write plan.md after it, x simulate an agent
// error, y simulate sync bringing a Message from Ben, F1-F3 variant, ctrl+c quit.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const width, height = 80, 24

// state is where a turn stands (see grill-state.html for the full model).
type state int

const (
	idle     state = iota // nothing running; Enter starts or continues
	thinking              // the agent's turn runs and streams text
	waiting               // the agent asked; the author types the answer
	failed                // the turn ended with an error
	ended                 // the agent wrote its summary
	writing               // the write-plan turn runs
)

var stateName = map[state]string{idle: "idle", thinking: "agent is thinking", waiting: "your answer", failed: "error", ended: "Grill finished", writing: "writing plan.md"}

// entry is one item of the conversation.
type entry struct {
	who  string // "ai", "you", "ben", "note"
	text string
	open bool // an open Question
}

// script is what the fake agent says, turn by turn.
var script = []string{
	"The success line \"Customers can pay by card\" restates the goal. What observable result would prove it worked, for example a test-card checkout that marks the order paid with nobody touching it?",
	"What happens to the order when a card payment fails: declined card, abandoned 3-D Secure, or the tab closed after the redirect to Stripe?",
	"Is any stock held while a card payment is pending, and for how long?",
}

const summary = "Summary: success is a test-card checkout that marks the order paid on its own, with 30% of orders by card within a month. A failed payment leaves the order unpaid and retryable; no stock is held. Open: nothing. Next: write plan.md (w)."

type model struct {
	variant  string
	st       state
	conv     []entry
	turn     int
	stream   string // text streamed so far in this turn
	input    string
	notice   string // one line above the input: sync news, errors
	sync     string
	thinkFor int // ticks of the spinner
}

type tickMsg struct{}

func tick() tea.Cmd { return tea.Tick(30*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} }) }

func newModel() model {
	return model{variant: "A", sync: "synced"}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.step()
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "ctrl+c":
			return m, tea.Quit
		case "f1", "f2", "f3":
			m.variant = map[string]string{"f1": "A", "f2": "B", "f3": "C"}[k]
		case "esc":
			switch m.st {
			case thinking, writing:
				m.st, m.stream, m.notice = idle, "", "Turn cancelled. Enter continues the Grill from where it stopped."
			default:
				return m, tea.Quit // the real screen returns to the plan detail
			}
		case "y":
			m.conv = append(m.conv, entry{who: "ben", text: "Should refunds really stay out?"})
			m.notice = "Sync brought 1 Message from Ben."
		case "x":
			if m.st == thinking {
				m.st, m.stream, m.notice = failed, "", "claude exited with status 1: rate limited. Enter tries the turn again."
			}
		case "f":
			if m.st == waiting || m.st == idle {
				m.st, m.stream = thinking, ""
				m.turn = len(script) // the next text is the summary
				return m, tick()
			}
		case "w":
			if m.st == ended {
				m.st, m.stream = writing, ""
				return m, tick()
			}
		case "enter":
			switch m.st {
			case idle, failed:
				m.st, m.notice = thinking, ""
				return m, tick()
			case waiting:
				if strings.TrimSpace(m.input) == "" {
					return m, nil
				}
				m.conv[len(m.conv)-1].open = false
				m.conv = append(m.conv, entry{who: "you", text: m.input})
				m.input, m.st = "", thinking
				return m, tick()
			}
		case "backspace":
			if m.st == waiting && m.input != "" {
				m.input = m.input[:len(m.input)-1]
			}
		case "space":
			if m.st == waiting {
				m.input += " "
			}
		default:
			if m.st == waiting && len(k) == 1 {
				m.input += k
			}
		}
	}
	return m, nil
}

// step streams the fake agent's text a few characters at a time.
func (m model) step() (tea.Model, tea.Cmd) {
	if m.st != thinking && m.st != writing {
		return m, nil
	}
	m.thinkFor++
	target := summary
	if m.st == writing {
		target = "Wrote plan.md with 7 acceptance criteria; bfc edit --check reported no problems."
	} else if m.turn < len(script) {
		target = script[m.turn]
	}
	if len(m.stream) < len(target) {
		m.stream = target[:min(len(target), len(m.stream)+3)]
		return m, tick()
	}
	switch {
	case m.st == writing:
		m.conv = append(m.conv, entry{who: "note", text: target})
		m.st, m.notice = idle, "plan.md saved. The real screen returns to the plan detail on plan.md."
	case m.turn >= len(script):
		m.conv = append(m.conv, entry{who: "ai", text: target})
		m.st = ended
	default:
		m.conv = append(m.conv, entry{who: "ai", text: target, open: true})
		m.turn++
		m.st = waiting
	}
	m.stream = ""
	return m, nil
}

var (
	bold   = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Faint(true)
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	cyan   = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	green  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	red    = lipgloss.NewStyle().Foreground(lipgloss.Red)
)

const intent = `Problem: customers cannot pay by card; we only take bank transfers.
Who: shop customers.
Worked when: customers can pay by card.
Out of scope: refunds.`

func (m model) View() tea.View {
	return tea.NewView(m.render())
}

func (m model) render() string {
	var lines []string
	switch m.variant {
	case "A":
		lines = m.chat(width)
	case "B":
		lines = m.split()
	default:
		lines = m.focus()
	}
	return strings.Join(fit(lines, height), "\n")
}

// header is the top line: screen, plan, and the round count.
func (m model) header() string {
	n := 0
	for _, e := range m.conv {
		if e.who == "ai" && e.text != summary {
			n++
		}
	}
	if m.stream != "" && m.st == thinking && m.turn < len(script) {
		n++
	}
	return bold.Render("Grill") + "  bfc-t8qq Payments via Stripe  " + dim.Render(fmt.Sprintf("draft · Question %d", max(n, 1)))
}

// status is the line above the help: turn state and sync.
func (m model) status() string {
	s := stateName[m.st]
	switch m.st {
	case thinking, writing:
		s = yellow.Render([]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}[m.thinkFor%8] + " " + s)
	case waiting:
		s = cyan.Render("▸ " + s)
	case failed:
		s = red.Render("✗ " + s)
	case ended:
		s = green.Render("✓ " + s)
	}
	return s + dim.Render("  ·  author: Ada Lovelace  ·  "+m.sync)
}

func (m model) helpLine() string {
	switch m.st {
	case thinking, writing:
		return dim.Render("esc cancel turn · ctrl+c quit")
	case waiting:
		return dim.Render("enter send answer · f finish Grill · esc back (Grill pauses)")
	case ended:
		return dim.Render("w write plan.md · enter keep grilling · esc back")
	default:
		return dim.Render("enter start/continue · f finish · esc back")
	}
}

// inputLine is the answer line, or the notice.
func (m model) inputLine() []string {
	var out []string
	if m.notice != "" {
		out = append(out, yellow.Render(m.notice))
	}
	if m.st == waiting {
		out = append(out, cyan.Render("› ")+m.input+"█")
	}
	return out
}

func wrap(s string, w int) []string { return strings.Split(ansi.Wrap(s, w, ""), "\n") }

// transcript renders the conversation in w columns, newest at the bottom.
func (m model) transcript(w int) []string {
	var out []string
	for _, e := range m.conv {
		var tag string
		switch e.who {
		case "ai":
			tag = cyan.Render("AI  ")
			if e.open {
				tag = cyan.Render("AI ?")
			}
		case "you":
			tag = green.Render("You ")
		case "ben":
			tag = yellow.Render("Ben ")
		default:
			tag = dim.Render("··· ")
		}
		for i, l := range wrap(e.text, w-5) {
			if i == 0 {
				out = append(out, tag+" "+l)
			} else {
				out = append(out, "     "+l)
			}
		}
		out = append(out, "")
	}
	if m.stream != "" {
		for i, l := range wrap(m.stream, w-5) {
			prefix := "     "
			if i == 0 {
				prefix = yellow.Render("AI … ")
			}
			out = append(out, prefix+l)
		}
	}
	if len(out) == 0 {
		out = append(out, dim.Render("No Questions yet. Enter starts the Grill: Claude Code reads the Intent"), dim.Render("and asks one Question per round; your answers are stored on the plan."))
	}
	return out
}

// A: the conversation over the full width.
func (m model) chat(w int) []string {
	foot := append(m.inputLine(), m.status(), m.helpLine())
	body := tail(m.transcript(w), height-2-len(foot))
	return append(append([]string{m.header(), ""}, pad(body, height-2-len(foot))...), foot...)
}

// B: the Intent in a left column, the conversation on the right.
func (m model) split() []string {
	const left = 26
	foot := append(m.inputLine(), m.status(), m.helpLine())
	rows := height - 2 - len(foot)
	l := pad(append([]string{bold.Render("intent.md")}, wrap(intent, left-1)...), rows)
	r := pad(tail(m.transcript(width-left-3), rows), rows)
	body := make([]string, rows)
	for i := range rows {
		body[i] = ansi.Truncate(l[i], left, "") + strings.Repeat(" ", max(0, left-ansi.StringWidth(l[i]))) + dim.Render(" │ ") + r[i]
	}
	return append(append([]string{m.header(), ""}, body...), foot...)
}

// C: the open Question large on top, the answered ones one line each below.
func (m model) focus() []string {
	var top []string
	var cur entry
	for _, e := range m.conv {
		if e.open || (e.who == "ai" && m.st == ended) {
			cur = e
		}
	}
	switch {
	case m.stream != "":
		top = append([]string{yellow.Render("Claude is asking…")}, wrap(m.stream, width-2)...)
	case cur.text != "":
		top = append([]string{bold.Render("Question")}, wrap(cur.text, width-2)...)
	default:
		top = []string{dim.Render("No open Question. Enter starts the Grill.")}
	}
	var hist []string
	n := 0
	for i, e := range m.conv {
		if e.who == "ai" && !e.open && i+1 < len(m.conv) && m.conv[i+1].who == "you" {
			n++
			hist = append(hist, ansi.Truncate(fmt.Sprintf("%d. %s", n, e.text), width, "…"),
				ansi.Truncate(green.Render("   → "+m.conv[i+1].text), width, "…"))
		}
		if e.who == "ben" {
			hist = append(hist, ansi.Truncate(yellow.Render("Ben: "+e.text), width, "…"))
		}
	}
	foot := append(m.inputLine(), m.status(), m.helpLine())
	rows := height - 2 - len(foot)
	body := append(top, "", dim.Render(fmt.Sprintf("── answered (%d) ──", n)))
	body = append(body, tail(hist, max(0, rows-len(body)))...)
	return append(append([]string{m.header(), ""}, pad(body, rows)...), foot...)
}

func tail(lines []string, n int) []string {
	if n <= 0 {
		return nil
	}
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func pad(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}

// fit cuts every line to the width and the screen to the height.
func fit(lines []string, h int) []string {
	lines = pad(lines, h)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return lines
}

// dump prints every variant in the states worth comparing, without styles.
func dump() {
	frames := []struct {
		name string
		make func() model
	}{
		{"before the first turn", func() model { return newModel() }},
		{"agent is streaming the 2nd Question", func() model {
			m := answered(1)
			m.st, m.stream = thinking, script[1][:70]
			return m
		}},
		{"waiting for the 3rd answer, sync brought a Message", func() model {
			m := answered(2)
			m.conv = append(m.conv, entry{who: "ben", text: "Should refunds really stay out?"})
			m.conv = append(m.conv, entry{who: "ai", text: script[2], open: true})
			m.turn, m.st, m.input, m.notice = 3, waiting, "No stock is held", "Sync brought 1 Message from Ben."
			return m
		}},
		{"Grill finished", func() model {
			m := answered(3)
			m.conv = append(m.conv, entry{who: "ai", text: summary})
			m.st = ended
			return m
		}},
	}
	for _, v := range []string{"A", "B", "C"} {
		for _, f := range frames {
			m := f.make()
			m.variant = v
			fmt.Printf("=== Variant %s — %s %s\n", v, f.name, strings.Repeat("=", max(0, 60-len(f.name))))
			fmt.Println(ansi.Strip(m.render()))
		}
	}
}

// answered returns a model after n answered rounds.
func answered(n int) model {
	m := newModel()
	answers := []string{"A test-card checkout turns the order paid on its own; 30% by card within a month.",
		"The order stays unpaid; the customer can retry by card or switch to bank transfer.",
		"We hold no stock."}
	for i := range n {
		m.conv = append(m.conv, entry{who: "ai", text: script[i]}, entry{who: "you", text: answers[i]})
	}
	m.turn = n
	return m
}

func main() {
	d := flag.Bool("dump", false, "print every variant in every state at 80x24 and exit")
	flag.Parse()
	if *d {
		dump()
		return
	}
	if _, err := tea.NewProgram(newModel()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
