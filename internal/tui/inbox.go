package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// Inbox is the start screen: what waits on the current user, in three
// sections (Findings on their plans, reviews they may approve, Questions
// addressed to them). Enter opens the plan of the selected item.
type Inbox struct {
	plans  Plans
	keys   inboxKeys
	help   help.Model
	width  int
	height int

	waiting core.Waiting
	loaded  bool
	loadErr error

	syncText string // set by the app: the sync status
	syncHint string // set by the app: why sync is offline or in conflict

	cursor int // index into items()
}

// NewInbox returns the Inbox screen. It loads when the program starts.
func NewInbox(plans Plans) Inbox {
	return Inbox{plans: plans, keys: newInboxKeys(), help: help.New()}
}

type inboxLoadedMsg struct {
	waiting core.Waiting
	err     error
}

// switchScreenMsg asks the app to switch between the Inbox and the plan
// list.
type switchScreenMsg struct{}

func (m Inbox) load() tea.Msg {
	w, err := m.plans.Inbox(context.Background())
	return inboxLoadedMsg{w, err}
}

// Init loads what waits.
func (m Inbox) Init() tea.Cmd { return m.load }

// inboxItem is one line of the Inbox that Enter can open.
type inboxItem struct {
	key     string // names the item across loads
	shortID string
	line    string
}

// sections returns the sections of the Inbox, each with its items.
func (m Inbox) sections() []struct {
	name  string
	items []inboxItem
} {
	var findings, reviews, questions []inboxItem
	for _, f := range m.waiting.Findings {
		key := fmt.Sprintf("finding %s %d %s %s", f.ShortID, f.Finding.PR, f.Finding.Criterion, f.Finding.OutOfScope)
		findings = append(findings, inboxItem{key, f.ShortID, fmt.Sprintf("%s  PR #%d  %s", f.ShortID, f.Finding.PR, f.Finding.Text())})
	}
	for _, r := range m.waiting.Reviews {
		reviews = append(reviews, inboxItem{"review " + r.ShortID, r.ShortID, r.ShortID + "  " + r.Title})
	}
	for _, q := range m.waiting.Questions {
		body, _, _ := strings.Cut(strings.TrimSpace(q.Message.Body), "\n")
		questions = append(questions, inboxItem{"question " + q.Message.ID.String(), q.ShortID, fmt.Sprintf("%s  from %s: %s", q.ShortID, q.Message.Author.Email, body)})
	}
	return []struct {
		name  string
		items []inboxItem
	}{{"Findings", findings}, {"Reviews", reviews}, {"Questions", questions}}
}

// items returns every item that Enter can open, in screen order.
func (m Inbox) items() []inboxItem {
	var out []inboxItem
	for _, s := range m.sections() {
		out = append(out, s.items...)
	}
	return out
}

// Update handles a message.
func (m Inbox) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
	case inboxLoadedMsg:
		before := m.items()
		m.waiting, m.loadErr, m.loaded = msg.waiting, msg.err, true
		m.cursor = keepSelection(before, m.items(), m.cursor, func(it inboxItem) string { return it.key })
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Up):
			m.cursor = max(m.cursor-1, 0)
		case key.Matches(msg, m.keys.Down):
			m.cursor = min(m.cursor+1, max(len(m.items())-1, 0))
		case key.Matches(msg, m.keys.Open):
			if items := m.items(); len(items) > 0 {
				short := items[m.cursor].shortID
				return m, func() tea.Msg { return openPlanMsg{short} }
			}
		case key.Matches(msg, m.keys.Plans):
			return m, func() tea.Msg { return switchScreenMsg{} }
		}
	}
	return m, nil
}

// View draws the screen: a header line, the sections, and the help line on
// the bottom row. Every line is cut to the width.
func (m Inbox) View() tea.View {
	header := []string{m.header(), ""}
	var footer []string
	if m.syncHint != "" {
		footer = append(footer, styleError.Render(m.syncHint))
	}
	footer = append(footer, m.help.View(m.keys))
	bodyHeight := max(m.height-len(header)-len(footer), 1)
	body, focus := m.body()
	start := min(max(focus-bodyHeight+1, 0), max(len(body)-bodyHeight, 0))
	body = body[start:min(len(body), start+bodyHeight)]
	for len(body) < bodyHeight {
		body = append(body, "")
	}
	lines := append(append(header, body...), footer...)
	for i := range lines {
		lines[i] = truncate(lines[i], m.width)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}

func (m Inbox) header() string {
	return withSyncText(styleTitle.Render("BeforeCode · inbox")+"   "+styleDim.Render("what waits on you"), m.syncText, m.width)
}

// body returns the lines below the header and the index of the line of the
// selected item.
func (m Inbox) body() ([]string, int) {
	var nse *NotSetUpError
	switch {
	case !m.loaded:
		return []string{"  Loading…"}, 0
	case errors.As(m.loadErr, &nse):
		return notSetUp(nse.Advice, m.width), 0
	case m.loadErr != nil:
		return []string{styleError.Render("  " + m.loadErr.Error())}, 0
	case m.waiting.Empty():
		return []string{"  Nothing waits on you. Tab shows the plans."}, 0
	}
	var lines []string
	focus, n := 0, 0
	for _, s := range m.sections() {
		if len(s.items) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		title := fmt.Sprintf("%s (%d) ", s.name, len(s.items))
		lines = append(lines, styleTitle.Render(title)+styleDim.Render(strings.Repeat("─", max(m.width-lipgloss.Width(title), 0))))
		for _, it := range s.items {
			line := "  " + it.line
			if n == m.cursor {
				line = styleSelected.Render("▶ " + it.line)
				focus = len(lines)
			}
			lines = append(lines, line)
			n++
		}
	}
	return lines, focus
}
