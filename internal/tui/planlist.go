package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// PlanRow is what the plan list shows of one Plan.
type PlanRow struct {
	ShortID      string
	State        core.State
	Title        string
	Author       string // email; empty for an invalid plan
	LastActivity time.Time
}

// Plans is where the screens get plans from and create them. cmd/bfc
// implements it on the Plans ref; tests use a fake.
type Plans interface {
	// Load returns every plan, newest activity first. It returns a
	// *NotSetUpError when the repository has no Plans ref.
	Load(ctx context.Context) ([]PlanRow, error)
	// NewPlan returns a command that creates a plan with this title, the
	// way bfc new does (the editor opens on intent.md), and then produces a
	// PlanCreatedMsg.
	NewPlan(title string) tea.Cmd
}

// PlanCreatedMsg reports the end of a NewPlan command. Err is nil when a
// plan was created.
type PlanCreatedMsg struct{ Err error }

// NotSetUpError means the repository has no Plans ref. Advice, when set, is
// the CLI's precise advice, for example that the remote already has plans.
type NotSetUpError struct{ Advice string }

func (e *NotSetUpError) Error() string {
	if e.Advice == "" {
		return "BeforeCode is not set up in this repository"
	}
	return e.Advice
}

// stateFilter is one step of the state filter that the s key cycles.
type stateFilter struct {
	name  string
	match func(core.State) bool
}

var stateFilters = []stateFilter{
	{"open", func(s core.State) bool { return s != core.StateClosed }},
	{"in-review", func(s core.State) bool { return s == core.StateInReview }},
	{"approved", func(s core.State) bool { return s == core.StateApproved }},
	{"draft", func(s core.State) bool { return s == core.StateDraft }},
	{"closed", func(s core.State) bool { return s == core.StateClosed }},
	{"all", func(core.State) bool { return true }},
}

// groupOrder is the order of the state groups on the screen. Phase 3 adds
// in-progress and done here.
var groupOrder = []core.State{
	core.StateInReview, core.StateApproved, core.StateDraft, core.StateClosed, core.StateInvalid,
}

// typing says which line the keyboard is typing into.
type typing int

const (
	typingNothing typing = iota
	typingSearch
	typingTitle
)

// PlanList is the plan list screen: the plans grouped by state, with a
// text search and a state filter.
type PlanList struct {
	plans  Plans
	keys   listKeys
	help   help.Model
	width  int
	height int

	all     []PlanRow
	loaded  bool
	loadErr error
	status  string // one line of feedback, such as a failed bfc new

	cursor    int // index into visible()
	filterAt  int // index into stateFilters
	search    string
	typing    typing
	planTitle string // the title being typed after n
}

// NewPlanList returns the plan list screen. It loads the plans when the
// program starts.
func NewPlanList(plans Plans) PlanList {
	return PlanList{plans: plans, keys: newListKeys(), help: help.New()}
}

type plansLoadedMsg struct {
	plans []PlanRow
	err   error
}

func (m PlanList) load() tea.Msg {
	plans, err := m.plans.Load(context.Background())
	return plansLoadedMsg{plans, err}
}

// Init loads the plans.
func (m PlanList) Init() tea.Cmd { return m.load }

// Update handles a message.
func (m PlanList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
	case plansLoadedMsg:
		m.all, m.loadErr, m.loaded = msg.plans, msg.err, true
		m.cursor = min(m.cursor, max(len(m.visible())-1, 0))
	case PlanCreatedMsg:
		m.status = ""
		if msg.Err != nil {
			m.status = "bfc new: " + msg.Err.Error()
		}
		return m, m.load
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

func (m PlanList) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.typing {
	case typingSearch:
		if done, _ := editLine(&m.search, msg); done {
			m.typing = typingNothing
		}
		m.cursor = 0
		return m, nil
	case typingTitle:
		done, cancelled := editLine(&m.planTitle, msg)
		if !done {
			return m, nil
		}
		title := strings.TrimSpace(m.planTitle)
		m.typing, m.planTitle = typingNothing, ""
		if cancelled || title == "" {
			return m, nil
		}
		return m, m.plans.NewPlan(title)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.cursor = min(m.cursor+1, max(len(m.visible())-1, 0))
	case key.Matches(msg, m.keys.Search):
		m.typing = typingSearch
	case key.Matches(msg, m.keys.ClearSearch):
		m.search, m.cursor = "", 0
	case key.Matches(msg, m.keys.StateFilter):
		m.filterAt = (m.filterAt + 1) % len(stateFilters)
		m.cursor = 0
	case key.Matches(msg, m.keys.New):
		m.typing, m.status = typingTitle, ""
	}
	return m, nil
}

// editLine applies a key press to a line being typed. Enter finishes it;
// esc finishes it and clears it, which also reports cancelled.
func editLine(line *string, msg tea.KeyPressMsg) (done, cancelled bool) {
	switch msg.String() {
	case "enter":
		return true, false
	case "esc":
		*line = ""
		return true, true
	case "backspace":
		if r := []rune(*line); len(r) > 0 {
			*line = string(r[:len(r)-1])
		}
	default:
		*line += msg.Text
	}
	return false, false
}

// visible returns the plans that pass the filters, in screen order: by
// group, and within a group in the loaded order (newest activity first).
func (m PlanList) visible() []PlanRow {
	var out []PlanRow
	q := strings.ToLower(m.search)
	for _, st := range groupOrder {
		for _, p := range m.all {
			if p.State != st || !stateFilters[m.filterAt].match(p.State) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.ShortID+" "+p.Author), q) {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// View draws the screen: a header line, the body, and the help line on the
// bottom row. Every line is cut to the width.
func (m PlanList) View() tea.View {
	header := []string{m.header(), ""}
	footer := m.footer()
	bodyHeight := max(m.height-len(header)-len(footer), 1)
	body, focus := m.body()
	// Scroll just enough to keep the selected plan in view.
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

func (m PlanList) header() string {
	search := m.search
	switch {
	case m.typing == typingSearch:
		search += "▌"
	case search == "":
		search = styleDim.Render("(/ to search)")
	}
	return styleTitle.Render("BeforeCode · plans") + "   state: " + stateFilters[m.filterAt].name + "   search: " + search
}

func (m PlanList) footer() []string {
	var lines []string
	if m.typing == typingTitle {
		lines = append(lines, "New plan title: "+m.planTitle+"▌  "+styleDim.Render("(enter: open the editor, esc: cancel)"))
	}
	if m.status != "" {
		lines = append(lines, styleError.Render(m.status))
	}
	return append(lines, m.help.View(m.keys))
}

// body returns the lines below the header and the index of the line of the
// selected plan.
func (m PlanList) body() ([]string, int) {
	var nse *NotSetUpError
	switch {
	case !m.loaded:
		return []string{"  Loading plans…"}, 0
	case errors.As(m.loadErr, &nse):
		return notSetUp(nse.Advice, m.width), 0
	case m.loadErr != nil:
		return []string{styleError.Render("  " + m.loadErr.Error())}, 0
	case len(m.all) == 0:
		return []string{
			"  No plans yet.",
			"",
			"  Press n to write the first intent,",
			"  or run bfc pull if your team already has plans.",
		}, 0
	}
	vis := m.visible()
	if len(vis) == 0 {
		return []string{"  No plans match. Esc clears the search, s changes the state filter."}, 0
	}
	var lines []string
	focus := 0
	for i := 0; i < len(vis); {
		st := vis[i].State
		n := 0
		for i+n < len(vis) && vis[i+n].State == st {
			n++
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, stateStyle(st).Bold(true).Render(fmt.Sprintf("%s (%d)", strings.ToUpper(string(st)), n)))
		for _, p := range vis[i : i+n] {
			title := "  " + p.Title
			if i == m.cursor {
				title = styleSelected.Render("▶ " + p.Title)
				focus = len(lines) + 1 // keep the second line in view too
			}
			author := p.Author
			if author == "" {
				author = "-"
			}
			lines = append(lines, title, "  "+styleDim.Render(p.ShortID+" · "+author+" · "+formatTime(p.LastActivity)))
			i++
		}
	}
	return lines, focus
}

// notSetUp lists what to run in a repository without a Plans ref, and the
// CLI's precise advice when there is one, wrapped to the width.
func notSetUp(advice string, width int) []string {
	lines := []string{
		"  BeforeCode is not set up in this repository.",
		"",
		"  bfc init        start using BeforeCode here",
		"  bfc bootstrap   get the plans of a team that already uses it",
	}
	if advice != "" {
		wrapped := lipgloss.NewStyle().Width(max(width-2, 20)).Render(advice)
		lines = append(lines, "")
		for _, l := range strings.Split(wrapped, "\n") {
			lines = append(lines, "  "+strings.TrimRight(l, " "))
		}
	}
	return lines
}

// formatTime shows a time in local time to the minute, as bfc list does.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}
