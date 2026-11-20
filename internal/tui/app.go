package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

// App is the root model of the TUI: it shows the plan list, the detail of
// one plan or its Grill, and owns the background sync. The screens know nothing about
// sync; the app syncs on a timer and after every local change, shows the
// status in the detail's status bar, and reloads the screens when a sync
// brings changes.
type App struct {
	plans  Plans
	syncer Syncer
	opts   AppOptions
	now    func() time.Time

	list   PlanList
	detail *PlanDetailScreen
	grill  *GrillScreen // the Grill of the detail's plan, over the detail
	sync   syncState
	width  int
	height int
}

// AppOptions configures the app.
type AppOptions struct {
	// Interval is the time between background syncs.
	Interval time.Duration
	// Local is local mode: nothing is synced.
	Local bool
	// Grill runs the Grill screen; nil leaves the Grill out.
	Grill Grill
}

// NewApp returns the TUI.
func NewApp(plans Plans, syncer Syncer, opts AppOptions) App {
	return App{
		plans: plans, syncer: syncer, opts: opts, now: time.Now,
		list: NewPlanList(plans), sync: newSyncState(opts.Local),
	}
}

type syncTickMsg struct{}

type syncDoneMsg struct{ result SyncResult }

func (m App) tickLater() tea.Cmd {
	return tea.Tick(m.opts.Interval, func(time.Time) tea.Msg { return syncTickMsg{} })
}

func (m App) runSync() tea.Msg {
	return syncDoneMsg{m.syncer.Sync(context.Background())}
}

// Init loads the list and starts the first sync at once.
func (m App) Init() tea.Cmd {
	return tea.Batch(m.list.Init(), func() tea.Msg { return syncTickMsg{} })
}

// Update handles a message.
func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	start := false
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case syncTickMsg:
		m.sync, start = m.sync.tick()
		if !m.opts.Local {
			cmds = append(cmds, m.tickLater())
		}
	case syncDoneMsg:
		m.sync, start = m.sync.done(msg.result, m.now())
		if msg.result.Changed {
			cmds = append(cmds, m.list.load)
			if m.detail != nil {
				cmds = append(cmds, m.detail.load)
			}
			if m.grill != nil {
				cmds = append(cmds, m.grill.load)
			}
		}
	case PlanCreatedMsg:
		if msg.Err == nil {
			m.sync, start = m.sync.localChange()
		}
	case PlanEditedMsg:
		if msg.Err == nil && msg.Changed {
			m.sync, start = m.sync.localChange()
		}
		if m.grill != nil && m.detail != nil {
			// The Grill wrote plan.md: show it on the detail.
			m.grill = nil
			m.detail.doc, m.detail.rendered = DocPlan, ""
		}
	case GrillChangedMsg:
		m.sync, start = m.sync.localChange()
	case reviewDoneMsg:
		if msg.err == nil {
			m.sync, start = m.sync.localChange()
		}
	case openPlanMsg:
		d := NewPlanDetail(m.plans, msg.shortID)
		d.canGrill = m.opts.Grill != nil
		next, _ := d.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		d = next.(PlanDetailScreen)
		m.detail = &d
		return m, d.Init()
	case openGrillMsg:
		if m.opts.Grill == nil {
			return m, nil
		}
		g := NewGrill(m.opts.Grill, msg.shortID)
		next, _ := g.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		g = next.(GrillScreen)
		m.grill = &g
		return m, g.Init()
	case closeGrillMsg:
		m.grill = nil
		if m.detail != nil {
			return m, m.detail.load
		}
		return m, nil
	case backMsg:
		m.detail = nil
		return m, m.list.load
	}
	if start {
		cmds = append(cmds, m.runSync)
	}
	cmds = append(cmds, m.forward(msg)...)
	return m, tea.Batch(cmds...)
}

// forward hands a message to the screens: input to the one shown, size and
// results to the ones they belong to.
func (m *App) forward(msg tea.Msg) []tea.Cmd {
	toList, toDetail, toGrill := m.detail == nil, m.detail != nil && m.grill == nil, m.grill != nil
	switch msg.(type) {
	case tea.WindowSizeMsg:
		toList, toDetail = true, m.detail != nil
	case plansLoadedMsg, PlanCreatedMsg:
		toList, toDetail, toGrill = true, false, false
	case detailLoadedMsg, reviewDoneMsg, PlanEditedMsg:
		toList, toDetail, toGrill = false, m.detail != nil, false
	case grillLoadedMsg, grillEventMsg, grillStartedMsg, grillAnsweredMsg, grillTickMsg, GrillReloadMsg:
		toList, toDetail = false, false
	case syncTickMsg, syncDoneMsg, GrillChangedMsg:
		return nil
	}
	var cmds []tea.Cmd
	if toList {
		next, cmd := m.list.Update(msg)
		m.list = next.(PlanList)
		cmds = append(cmds, cmd)
	}
	if toDetail {
		next, cmd := m.detail.Update(msg)
		d := next.(PlanDetailScreen)
		m.detail = &d
		cmds = append(cmds, cmd)
	}
	if toGrill {
		next, cmd := m.grill.Update(msg)
		g := next.(GrillScreen)
		m.grill = &g
		cmds = append(cmds, cmd)
	}
	return cmds
}

// View draws the screen shown.
func (m App) View() tea.View {
	if m.grill != nil {
		g := *m.grill
		g.syncText = m.sync.label(m.now())
		return g.View()
	}
	if m.detail != nil {
		d := *m.detail
		d.syncText, d.syncHint = m.sync.label(m.now()), m.sync.hint()
		return d.View()
	}
	l := m.list
	l.syncText, l.syncHint = m.sync.label(m.now()), m.sync.hint()
	return l.View()
}
