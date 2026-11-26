package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
)

// App is the root model of the TUI: it shows the Inbox (the start screen)
// or the plan list, the detail of one plan or its Grill, and owns the
// background sync. The screens know nothing about
// sync; the app syncs on a timer and after every local change, shows the
// status in the detail's status bar, and reloads the screens when a sync
// brings changes. It also watches the tip of the local Plans ref and
// reloads the screens when another process, such as an agent running the
// CLI, moved it.
type App struct {
	plans  Plans
	syncer Syncer
	opts   AppOptions
	now    func() time.Time

	inbox  Inbox
	list   PlanList
	onList bool // the plan list is shown instead of the Inbox
	detail *PlanDetailScreen
	grill  *GrillScreen // the Grill of the detail's plan, over the detail
	sync   syncState
	watch  watchState
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
	// Watcher reads the tip of the local Plans ref; nil leaves watching
	// out.
	Watcher Watcher
	// WatchInterval is the time between two reads of the tip;
	// DefaultWatchInterval when zero.
	WatchInterval time.Duration
}

// DefaultWatchInterval is the time between two reads of the Plans ref's
// tip: a write by an agent shows within a few seconds, at the cost of one
// git rev-parse.
const DefaultWatchInterval = 2 * time.Second

// Watcher reads the tip of the local Plans ref, so that the app notices a
// write by another process. cmd/bfc implements it on the Plans ref; tests
// use a fake.
type Watcher interface {
	// Tip returns the commit the Plans ref points to, or "" when there is
	// no Plans ref.
	Tip(ctx context.Context) (string, error)
}

// NewApp returns the TUI.
func NewApp(plans Plans, syncer Syncer, opts AppOptions) App {
	if opts.WatchInterval <= 0 {
		opts.WatchInterval = DefaultWatchInterval
	}
	return App{
		plans: plans, syncer: syncer, opts: opts, now: time.Now,
		inbox: NewInbox(plans), list: NewPlanList(plans), sync: newSyncState(opts.Local),
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

// watchState is what the app knows about the tip of the Plans ref. The
// screens are reloaded only after a read of the tip, so they never show an
// older state than the tip recorded. One read runs at a time: a read asked
// for while one runs follows it, so an older read never overwrites a newer
// one.
type watchState struct {
	tip     string // the tip the screens were last loaded for
	known   bool   // tip was read at least once
	reading bool   // a read is in flight
	again   bool   // read once more when the running read ends
}

// watchTickMsg is the watch timer: read the tip.
type watchTickMsg struct{}

// watchDoneMsg is the result of one read of the tip.
type watchDoneMsg struct {
	tip string
	err error
}

func (m App) watchLater() tea.Cmd {
	return tea.Tick(m.opts.WatchInterval, func(time.Time) tea.Msg { return watchTickMsg{} })
}

// readTip starts a read of the tip, or queues one when a read runs.
func (m *App) readTip() tea.Cmd {
	if m.watch.reading {
		m.watch.again = true
		return nil
	}
	m.watch.reading = true
	w := m.opts.Watcher
	return func() tea.Msg {
		tip, err := w.Tip(context.Background())
		return watchDoneMsg{tip, err}
	}
}

// tipRead applies a read of the tip: the screens reload when it moved, and
// at the first read, which loads them in the first place.
func (m *App) tipRead(msg watchDoneMsg) []tea.Cmd {
	m.watch.reading = false
	var cmds []tea.Cmd
	switch {
	case msg.err != nil && !m.watch.known:
		m.watch.known = true
		cmds = m.reloadScreens() // the screens show the error
	case msg.err == nil && (!m.watch.known || msg.tip != m.watch.tip):
		m.watch.tip, m.watch.known = msg.tip, true
		cmds = m.reloadScreens()
	}
	if m.watch.again {
		m.watch.again = false
		cmds = append(cmds, m.readTip())
	}
	return cmds
}

// reloadScreens reloads every screen that is open.
func (m App) reloadScreens() []tea.Cmd {
	cmds := []tea.Cmd{m.inbox.load, m.list.load}
	if m.detail != nil {
		cmds = append(cmds, m.detail.load)
	}
	if m.grill != nil {
		cmds = append(cmds, func() tea.Msg { return reloadGrillMsg{} })
	}
	return cmds
}

// Init starts the first sync at once and loads the Inbox and the list:
// with a Watcher after the first read of the tip, so that a write between
// the load and the read is not missed.
func (m App) Init() tea.Cmd {
	syncNow := func() tea.Msg { return syncTickMsg{} }
	if m.opts.Watcher != nil {
		return tea.Batch(syncNow, func() tea.Msg { return watchTickMsg{} })
	}
	return tea.Batch(m.inbox.Init(), m.list.Init(), syncNow)
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
		switch {
		case msg.result.Changed && m.opts.Watcher != nil:
			// The read reloads once, unless the timer already did for
			// the tip the sync made.
			cmds = append(cmds, m.readTip())
		case msg.result.Changed:
			cmds = append(cmds, m.reloadScreens()...)
		}
	case watchTickMsg:
		return m, tea.Batch(m.readTip(), m.watchLater())
	case watchDoneMsg:
		return m, tea.Batch(m.tipRead(msg)...)
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
		return m, tea.Batch(m.inbox.load, m.list.load)
	case switchScreenMsg:
		m.onList = !m.onList
		return m, nil
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
	top := m.detail == nil
	toInbox, toList, toDetail, toGrill := top && !m.onList, top && m.onList, m.detail != nil && m.grill == nil, m.grill != nil
	switch msg.(type) {
	case tea.WindowSizeMsg:
		toInbox, toList, toDetail = true, true, m.detail != nil
	case inboxLoadedMsg:
		toInbox, toList, toDetail, toGrill = true, false, false, false
	case plansLoadedMsg, PlanCreatedMsg:
		toInbox, toList, toDetail, toGrill = false, true, false, false
	case detailLoadedMsg, reviewDoneMsg, PlanEditedMsg:
		toInbox, toList, toDetail, toGrill = false, false, m.detail != nil, false
	case grillLoadedMsg, grillEventMsg, grillStartedMsg, grillAnsweredMsg, grillTickMsg, reloadGrillMsg:
		toInbox, toList, toDetail = false, false, false
	case syncTickMsg, syncDoneMsg, GrillChangedMsg:
		return nil
	}
	var cmds []tea.Cmd
	if toInbox {
		next, cmd := m.inbox.Update(msg)
		m.inbox = next.(Inbox)
		cmds = append(cmds, cmd)
	}
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
	if !m.onList {
		in := m.inbox
		in.syncText, in.syncHint = m.sync.label(m.now()), m.sync.hint()
		return in.View()
	}
	l := m.list
	l.syncText, l.syncHint = m.sync.label(m.now()), m.sync.hint()
	return l.View()
}
