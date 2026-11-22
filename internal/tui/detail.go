package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

// PlanDetailScreen shows one plan: a header line with the document tabs,
// the document rendered with Glamour in a viewport, a status bar and the
// help line. It writes nothing itself: edits and review actions go through
// Plans, and the app syncs after them.
type PlanDetailScreen struct {
	plans   Plans
	shortID string
	keys    detailKeys
	help    help.Model
	view    viewport.Model
	width   int
	height  int

	detail  PlanDetail
	loaded  bool
	loadErr error
	doc     Doc
	status  string // the last action's result or refusal
	failed  bool   // status is a refusal or error
	// typing is the action whose reason is being typed on the line above
	// the status bar, "" when none: request changes, or close and done,
	// where the line is also the confirmation.
	typing   ReviewAction
	reason   string
	syncText string // set by the app before each View
	syncHint string // set by the app: why sync is offline or in conflict
	canGrill bool   // set by the app: the Grill is available
	rendered string // the document as last rendered, to keep the scroll
}

// NewPlanDetail returns the detail screen of the plan with this Short ID.
// It loads the plan when its Init runs.
func NewPlanDetail(plans Plans, shortID string) PlanDetailScreen {
	return PlanDetailScreen{plans: plans, shortID: shortID, keys: newDetailKeys(), help: help.New(), view: viewport.New()}
}

// backMsg asks the app to return to the list.
type backMsg struct{}

// openGrillMsg asks the app to open the Grill of the plan.
type openGrillMsg struct{ shortID string }

type detailLoadedMsg struct {
	detail PlanDetail
	err    error
}

// reviewDoneMsg reports a review action; a nil err is a local change.
type reviewDoneMsg struct {
	action ReviewAction
	err    error
}

func (m PlanDetailScreen) load() tea.Msg {
	d, err := m.plans.Detail(context.Background(), m.shortID)
	return detailLoadedMsg{d, err}
}

// Init loads the plan.
func (m PlanDetailScreen) Init() tea.Cmd { return m.load }

// Update handles a message.
func (m PlanDetailScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		m.refresh()
	case detailLoadedMsg:
		m.loaded = true
		m.loadErr = msg.err
		if msg.err == nil {
			first := m.detail.ShortID == ""
			m.detail = msg.detail
			if first && m.detail.Plan == nil {
				m.doc = DocIntent
			}
		}
		m.refresh()
	case reviewDoneMsg:
		m.failed = msg.err != nil
		m.status = string(msg.action) + ": done"
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		return m, m.load
	case PlanEditedMsg:
		m.failed = msg.Err != nil
		m.status = ""
		if msg.Err != nil {
			m.status = "edit: " + msg.Err.Error()
		}
		return m, m.load
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

func (m PlanDetailScreen) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.typing != "" {
		done, cancelled := editLine(&m.reason, msg)
		if !done {
			return m, nil
		}
		a, reason := m.typing, strings.TrimSpace(m.reason)
		m.typing, m.reason = "", ""
		// Request changes needs a reason; for close it is optional.
		if cancelled || reason == "" && a == ActionRequestChanges {
			return m, nil
		}
		return m, m.review(a, reason)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		return m, func() tea.Msg { return backMsg{} }
	case key.Matches(msg, m.keys.SwitchDoc):
		m.doc = m.doc.other()
		m.rendered = ""
		m.refresh()
		m.view.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Grill) && m.canGrill && m.detail.Editable && m.loadErr == nil:
		return m, func() tea.Msg { return openGrillMsg{m.shortID} }
	case key.Matches(msg, m.keys.Edit) && m.detail.Editable && m.loadErr == nil:
		m.status = ""
		return m, m.plans.Edit(m.shortID, m.doc)
	}
	for _, a := range m.detail.Actions {
		if !key.Matches(msg, m.keys.Review[a]) {
			continue
		}
		if a == ActionRequestChanges || a == ActionClose || a == ActionDone {
			m.typing, m.status = a, ""
			return m, nil
		}
		return m, m.review(a, "")
	}
	var cmd tea.Cmd
	m.view, cmd = m.view.Update(msg)
	return m, cmd
}

func (m PlanDetailScreen) review(a ReviewAction, reason string) tea.Cmd {
	plans, short := m.plans, m.shortID
	return func() tea.Msg {
		return reviewDoneMsg{a, plans.Review(context.Background(), short, a, reason)}
	}
}

// footer returns the lines below the document.
func (m PlanDetailScreen) footer() []string {
	var lines []string
	switch m.typing {
	case ActionRequestChanges:
		lines = append(lines, "Reason: "+m.reason+"▌  "+styleDim.Render("(enter: request changes, esc: cancel)"))
	case ActionClose:
		lines = append(lines, "Close "+m.shortID+"? Reason (optional): "+m.reason+"▌  "+styleDim.Render("(enter: close, esc: cancel)"))
	case ActionDone:
		lines = append(lines, "Mark "+m.shortID+" done? It is final. Note (optional): "+m.reason+"▌  "+styleDim.Render("(enter: done, esc: cancel)"))
	}
	if n := m.detail.BlockingQuestions; n > 0 && m.typing == "" {
		lines = append(lines, styleWarn.Render("Approval waits on "+plural(n, "open Question")+"; bfc show "+m.shortID+" lists them."))
	}
	if m.status != "" {
		style := styleDim
		if m.failed {
			style = styleError
		}
		lines = append(lines, style.Render(m.status))
	}
	if m.syncHint != "" {
		lines = append(lines, styleError.Render(m.syncHint))
	}
	return append(lines, m.statusBar(), m.help.ShortHelpView(m.helpKeys()))
}

// helpKeys lists the keys valid now.
func (m PlanDetailScreen) helpKeys() []key.Binding {
	keys := []key.Binding{m.keys.SwitchDoc}
	if m.detail.Editable {
		keys = append(keys, m.keys.Edit)
		if m.canGrill {
			keys = append(keys, m.keys.Grill)
		}
	}
	for _, a := range reviewActionOrder {
		if slices.Contains(m.detail.Actions, a) {
			keys = append(keys, m.keys.Review[a])
		}
	}
	return append(keys, m.keys.Scroll, m.keys.Back, m.keys.Quit)
}

// statusBar shows state, Approvals, author and sync in reverse video.
func (m PlanDetailScreen) statusBar() string {
	d := m.detail
	parts := []string{string(d.State)}
	if d.RequiredApprovals > 0 {
		a := fmt.Sprintf("%d/%d approvals", len(d.Approvers), d.RequiredApprovals)
		if len(d.Approvers) > 0 {
			names := make([]string, len(d.Approvers))
			for i, e := range d.Approvers {
				names[i], _, _ = strings.Cut(e, "@")
			}
			a += " (" + strings.Join(names, ", ") + ")"
		}
		parts = append(parts, a)
	}
	if d.Author != "" {
		parts = append(parts, d.Author)
	}
	// The sync status keeps its place; the plan's facts give way.
	right := " " + m.syncText + " "
	left := truncate(" "+strings.Join(parts, " · "), max(m.width-lipgloss.Width(right), 1))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return styleSelected.Render(truncate(left+strings.Repeat(" ", gap)+right, m.width))
}

func (m PlanDetailScreen) header() string {
	tabs := make([]string, 2)
	for i, d := range []Doc{DocIntent, DocPlan} {
		tabs[i] = styleDim.Render(d.String())
		if d == m.doc {
			tabs[i] = styleTab.Render(d.String())
		}
	}
	return styleTitle.Render(m.shortID+"  "+m.detail.Title) + "   " + tabs[0] + "   " + tabs[1]
}

// bodyHeight is the height of the viewport: what the header and footer
// leave.
func (m PlanDetailScreen) bodyHeight() int {
	return max(m.height-1-len(m.footer()), 1)
}

// refresh sizes the viewport and renders the shown document again with
// Glamour, keeping the scroll position. It runs on resize, load and
// document switch only; View never renders Markdown.
func (m *PlanDetailScreen) refresh() {
	m.view.SetWidth(m.width)
	m.view.SetHeight(m.bodyHeight())
	content := m.render()
	if content == m.rendered {
		return
	}
	offset := m.view.YOffset()
	m.view.SetContent(content)
	m.view.SetYOffset(offset)
	m.rendered = content
}

func (m PlanDetailScreen) render() string {
	switch {
	case !m.loaded:
		return "  Loading " + m.shortID + "…"
	case m.loadErr != nil:
		return styleError.Render("  " + m.loadErr.Error())
	}
	text := m.detail.Plan
	if m.doc == DocIntent {
		text = m.detail.Intent
	}
	if text == nil {
		return fmt.Sprintf("  No %s yet. Press e to write it.", m.doc)
	}
	return renderMarkdown(withoutFrontMatter(string(text)), m.width)
}

// withoutFrontMatter drops the YAML front matter of intent.md; its title
// is already in the header.
func withoutFrontMatter(doc string) string {
	rest, ok := strings.CutPrefix(doc, "---\n")
	if !ok {
		return doc
	}
	if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
		return body
	}
	return doc
}

// renderMarkdown renders a document with Glamour for the given width.
func renderMarkdown(md string, width int) string {
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(max(width-4, 20)))
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return strings.Trim(out, "\n")
}

// View draws the screen; the app sets the sync text first.
func (m PlanDetailScreen) View() tea.View {
	m.view.SetHeight(m.bodyHeight())
	lines := append([]string{m.header()}, strings.Split(m.view.View(), "\n")...)
	lines = append(lines, m.footer()...)
	for i := range lines {
		lines[i] = truncate(lines[i], m.width)
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	return v
}
