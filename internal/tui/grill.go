package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// Grill is how the Grill screen reads a plan's Grill, runs the agent and
// stores the author's answers. cmd/bfc implements it with the agent adapter
// and the code of bfc ask and bfc reply (ADR 0012); tests use a fake.
type Grill interface {
	// Snapshot returns the plan's Grill so far.
	Snapshot(ctx context.Context, shortID string) (GrillSnapshot, error)
	// Turn starts one turn of the agent. The channel carries the agent's
	// text as it streams, and an Err when the turn fails, including when
	// ctx is cancelled; it closes when the turn has ended.
	Turn(ctx context.Context, shortID string, t GrillTurn) (<-chan GrillEvent, error)
	// Answer stores the author's answer to the Question, like bfc reply
	// --verbatim: the author's own words.
	Answer(ctx context.Context, shortID, questionID, text string) error
}

// GrillTurnKind is what a turn of the agent is for.
type GrillTurnKind int

// The kinds of turn.
const (
	TurnAsk       GrillTurnKind = iota // ask the next Question
	TurnSummary                        // finish the Grill with a summary
	TurnWritePlan                      // write plan.md
)

// GrillTurn is one turn of the agent.
type GrillTurn struct {
	Kind GrillTurnKind
	// Answer is the author's answer to the previous Question, for a
	// TurnAsk that follows one; empty to continue without one.
	Answer string
	// Force lets a TurnWritePlan replace a written plan.md: the author
	// said yes.
	Force bool
}

// GrillEvent is one piece of a turn's output.
type GrillEvent struct {
	Text string // the agent's text, a piece at a time
	Tool string // a command the agent runs now, one line
	Err  error  // the turn failed
	// PlanSaved reports that a TurnWritePlan stored a new plan.md.
	PlanSaved bool
}

// GrillSnapshot is what the Grill screen shows of a plan.
type GrillSnapshot struct {
	ShortID, Title string
	State          core.State
	Author         string // the author's name
	AuthorEmail    string
	// Questions are the Grill's Questions from the AI to the author, in
	// the order they were asked.
	Questions []GrillQuestion
	// Others are Messages that neither ask nor answer a Grill Question,
	// such as comments of other people, oldest first.
	Others []GrillMessage
	// PlanWritten is true when plan.md is no longer the template.
	PlanWritten bool
}

// GrillQuestion is a Question of the Grill and its answer.
type GrillQuestion struct {
	ID     string
	Text   string
	Answer string // the author's answer, "" while open
	Open   bool
}

// GrillMessage is a Message outside the Grill's Questions and their
// answers, such as a comment by someone else.
type GrillMessage struct{ Who, Text string }

// open returns the newest open Question, or nil.
func (s GrillSnapshot) open() *GrillQuestion {
	for i := len(s.Questions) - 1; i >= 0; i-- {
		if s.Questions[i].Open {
			return &s.Questions[i]
		}
	}
	return nil
}

// grillPhase is where the Grill stands (docs/design.md, section 3).
type grillPhase int

const (
	phaseIdle     grillPhase = iota // nothing runs; Enter starts or continues
	phaseThinking                   // a turn runs and streams
	phaseWaiting                    // the author types the answer
	phaseSaving                     // the answer is being stored
	phaseError                      // the last turn failed; Enter retries it
	phaseEnded                      // the agent wrote its summary
	phaseWriting                    // the write-plan turn runs
	phaseDone                       // the agent asked nothing: nothing is left to ask
)

// GrillScreen is the Grill of one plan: one Question at a time, with the
// answered ones below it. It writes through Grill only; the app syncs
// after its GrillChangedMsg and PlanEditedMsg.
type GrillScreen struct {
	grill   Grill
	shortID string
	keys    grillKeys
	help    help.Model
	width   int
	height  int

	snap    GrillSnapshot
	loaded  bool
	loadErr error

	phase   grillPhase
	turn    GrillTurn // the running or last turn
	stream  string    // the agent's text of the running turn
	tool    string    // the command the agent runs now
	summary string    // the agent's summary, once finished
	input   string    // the answer being typed
	notice  string    // one line of news: sync, errors, cancels
	failed  bool      // notice is an error
	confirm bool      // asking whether to replace plan.md
	cancel  context.CancelFunc
	// stopped is set when the author cancelled the running turn, quitting
	// when ctrl+c waits for it to end before bfc quits.
	stopped, quitting bool
	turnErr           error // the running turn failed
	planSaved         bool  // the running turn stored plan.md
	// settling is the number of the snapshot that decides the phase after
	// a turn; 0 when none is awaited. Only that snapshot decides, not one a
	// sync asked for earlier.
	settling, loads int
	frame           int    // of the spinner
	syncText        string // set by the app before each View
}

// NewGrill returns the Grill screen of the plan with this Short ID.
func NewGrill(grill Grill, shortID string) GrillScreen {
	return GrillScreen{grill: grill, shortID: shortID, keys: newGrillKeys(), help: help.New(), width: 80, height: 24}
}

// GrillChangedMsg reports that the Grill wrote to the plan: a Question or an
// answer. The app syncs after it.
type GrillChangedMsg struct{}

// reloadGrillMsg asks the screen to read the plan's Grill again; the app
// sends it after a sync brought changes.
type reloadGrillMsg struct{}

// closeGrillMsg asks the app to return to the plan detail.
type closeGrillMsg struct{}

type grillLoadedMsg struct {
	snap GrillSnapshot
	err  error
	n    int // the number of the load, see settling
}

// grillEventMsg carries one event of a running turn; done is set when the
// turn's channel closed.
type grillEventMsg struct {
	ev   GrillEvent
	ch   <-chan GrillEvent
	done bool
}

type grillStartedMsg struct {
	ch  <-chan GrillEvent
	err error
}

type grillAnsweredMsg struct{ err error }

type grillTickMsg struct{}

// load reads the plan's Grill as load number n.
func (m GrillScreen) load(n int) tea.Cmd {
	g, short := m.grill, m.shortID
	return func() tea.Msg {
		s, err := g.Snapshot(context.Background(), short)
		return grillLoadedMsg{s, err, n}
	}
}

// reload is a load that decides nothing.
func (m *GrillScreen) reload() tea.Cmd {
	m.loads++
	return m.load(m.loads)
}

// settle loads the snapshot that decides the phase after a turn.
func (m *GrillScreen) settle() tea.Cmd {
	m.loads++
	m.settling = m.loads
	return tea.Batch(m.load(m.loads), grillChanged)
}

// Init loads the plan's Grill.
func (m GrillScreen) Init() tea.Cmd { return m.load(0) }

func nextGrillEvent(ch <-chan GrillEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return grillEventMsg{ev: ev, ch: ch, done: !ok}
	}
}

func tickGrillSpinner() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return grillTickMsg{} })
}

func grillChanged() tea.Msg { return GrillChangedMsg{} }

func (m GrillScreen) running() bool { return m.phase == phaseThinking || m.phase == phaseWriting }

// start runs a turn.
func (m GrillScreen) start(t GrillTurn) (GrillScreen, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.turn, m.stream, m.tool, m.confirm = cancel, t, "", "", false
	m.stopped, m.turnErr, m.planSaved = false, nil, false
	m.phase = phaseThinking
	if t.Kind == TurnWritePlan {
		m.phase = phaseWriting
	}
	m.notice, m.failed = "", false
	g, short := m.grill, m.shortID
	return m, tea.Batch(func() tea.Msg {
		ch, err := g.Turn(ctx, short, t)
		return grillStartedMsg{ch, err}
	}, tickGrillSpinner())
}

// Update handles a message.
func (m GrillScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
	case grillLoadedMsg:
		return m.loadedSnap(msg)
	case reloadGrillMsg:
		return m, m.reload()
	case grillStartedMsg:
		if msg.err != nil {
			m.turnErr = msg.err
			return m.endTurn()
		}
		return m, nextGrillEvent(msg.ch)
	case grillEventMsg:
		return m.event(msg)
	case grillAnsweredMsg:
		if msg.err != nil {
			m.phase, m.notice, m.failed = phaseWaiting, "answer not saved: "+msg.err.Error(), true
			return m, nil
		}
		answer := m.input
		m.input = ""
		m, cmd := m.start(GrillTurn{Kind: TurnAsk, Answer: answer})
		return m, tea.Batch(cmd, m.reload(), grillChanged)
	case grillTickMsg:
		if m.running() {
			m.frame++
			return m, tickGrillSpinner()
		}
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

// loadedSnap takes a new snapshot; the one a turn's end asked for decides
// what comes next.
func (m GrillScreen) loadedSnap(msg grillLoadedMsg) (tea.Model, tea.Cmd) {
	m.loaded, m.loadErr = true, msg.err
	if msg.err != nil {
		return m, nil
	}
	before := m.snap
	m.snap = msg.snap
	if n := len(m.snap.Others) - len(before.Others); n > 0 && before.ShortID != "" {
		m.notice, m.failed = fmt.Sprintf("%d new Message(s) arrived.", n), false
	}
	open := m.snap.open()
	switch {
	case m.settling != 0 && msg.n == m.settling:
		m.settling = 0
		m.decide(open != nil)
	case m.settling != 0 || m.running():
		// The turn decides once it has ended.
	case m.phase == phaseWaiting && open == nil:
		m.phase, m.input = phaseIdle, ""
		m.notice = strings.TrimSpace(m.notice + " The open Question was answered elsewhere.")
		m.failed = false
	case m.phase == phaseIdle && open != nil && msg.n == 0:
		m.phase = phaseWaiting // a Question from an earlier session waits
	}
	return m, nil
}

// decide sets the phase after a turn that asked for a Question, was
// cancelled or failed, from whether a Question is open now.
func (m *GrillScreen) decide(open bool) {
	switch {
	case open:
		// Whatever happened, the recorded Question waits for its answer.
		m.phase = phaseWaiting
	case m.turnErr != nil:
		m.phase = phaseError
	case m.stopped:
		m.phase = phaseIdle
	default:
		// No Question: the agent has nothing left to ask (#92).
		m.phase = phaseDone
	}
}

// event handles one event of the running turn.
func (m GrillScreen) event(msg grillEventMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.done:
		return m.endTurn()
	case msg.ev.Err != nil:
		if !m.stopped {
			m.turnErr = msg.ev.Err
		}
	case msg.ev.PlanSaved:
		m.planSaved = true
	default:
		m.stream += msg.ev.Text
		if msg.ev.Tool != "" {
			m.tool = msg.ev.Tool
		}
	}
	return m, nextGrillEvent(msg.ch)
}

// endTurn decides what comes after a turn: it ran to its end, failed, or
// was cancelled. A turn that reached its goal counts even when the cancel
// came too late to stop it.
func (m GrillScreen) endTurn() (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.quitting {
		return m, tea.Quit
	}
	switch {
	case m.turnErr != nil:
		m.notice, m.failed = m.turnErr.Error(), true
	case m.stopped:
		m.notice, m.failed = "Turn cancelled.", false
	}
	switch m.turn.Kind {
	case TurnWritePlan:
		switch {
		case m.planSaved:
			return m, func() tea.Msg { return PlanEditedMsg{Changed: true} }
		case m.stopped && m.turnErr == nil:
			m.phase = phaseEnded
		case m.turnErr == nil:
			m.phase, m.notice, m.failed = phaseError, "The turn ended without saving plan.md.", true
		default:
			m.phase = phaseError
		}
		return m, nil
	case TurnSummary:
		if m.turnErr == nil && !m.stopped {
			m.phase, m.summary = phaseEnded, strings.TrimSpace(m.stream)
			return m, nil
		}
	}
	return m, m.settle()
}

func (m GrillScreen) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.running() && m.cancel != nil {
			// Stop the agent and quit once it has ended.
			m.stopped, m.quitting = true, true
			m.cancel()
			return m, nil
		}
		return m, tea.Quit
	case m.confirm:
		switch {
		case key.Matches(msg, m.keys.Yes):
			return m.start(GrillTurn{Kind: TurnWritePlan, Force: true})
		case key.Matches(msg, m.keys.No):
			m.confirm = false
		}
		return m, nil
	case m.running():
		if key.Matches(msg, m.keys.Cancel) && m.cancel != nil {
			m.stopped = true
			m.cancel()
		}
		return m, nil
	case m.phase == phaseSaving || !m.loaded:
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		return m, func() tea.Msg { return closeGrillMsg{} }
	case m.phase == phaseWaiting:
		switch {
		case key.Matches(msg, m.keys.FinishTyping):
			return m.start(GrillTurn{Kind: TurnSummary})
		case key.Matches(msg, m.keys.Send):
			if strings.TrimSpace(m.input) == "" {
				return m, nil
			}
			q, g, short, text := m.snap.open(), m.grill, m.shortID, strings.TrimSpace(m.input)
			m.input, m.phase = text, phaseSaving
			return m, func() tea.Msg { return grillAnsweredMsg{g.Answer(context.Background(), short, q.ID, text)} }
		}
		editLine(&m.input, msg)
		return m, nil
	case key.Matches(msg, m.keys.Send) && m.phase == phaseError:
		return m.start(m.turn) // retry the turn that failed
	case key.Matches(msg, m.keys.Send):
		return m.start(GrillTurn{Kind: TurnAsk})
	case key.Matches(msg, m.keys.Finish) && m.phase != phaseEnded && (len(m.snap.Questions) > 0 || m.phase == phaseDone):
		return m.start(GrillTurn{Kind: TurnSummary})
	case key.Matches(msg, m.keys.Write) && m.phase == phaseEnded:
		if m.snap.PlanWritten {
			m.confirm = true
			return m, nil
		}
		return m.start(GrillTurn{Kind: TurnWritePlan})
	}
	return m, nil
}

// View draws the screen: header, the Question, the answered ones, then
// the notice, the answer line, the status bar and the help.
func (m GrillScreen) View() tea.View {
	if !m.loaded || m.loadErr != nil {
		text := "Loading " + m.shortID + "…"
		if m.loadErr != nil {
			text = styleError.Render(m.loadErr.Error())
		}
		lines := pad([]string{truncate(text, m.width)}, max(m.height-1, 1))
		lines = append(lines, truncate(m.help.ShortHelpView([]key.Binding{m.keys.Cancel, m.keys.Quit}), m.width))
		return tea.NewView(strings.Join(lines, "\n"))
	}
	w := m.width
	foot := m.footer()
	rows := max(m.height-2-len(foot), 1)
	body := append(m.focus(w), "")
	answered := 0
	for _, q := range m.snap.Questions {
		if !q.Open {
			answered++
		}
	}
	body = append(body, styleDim.Render(fmt.Sprintf("── answered (%d) ──", answered)))
	body = append(body, tail(m.history(w), max(rows-len(body), 0))...)
	lines := append([]string{m.header(), ""}, pad(body, rows)...)
	lines = append(lines, foot...)
	for i, l := range lines {
		lines[i] = truncate(l, w)
	}
	return tea.NewView(strings.Join(lines, "\n"))
}

func (m GrillScreen) header() string {
	n := len(m.snap.Questions)
	if m.phase == phaseThinking && m.turn.Kind == TurnAsk && m.snap.open() == nil {
		n++ // the Question being asked
	}
	return styleTitle.Render("Grill") + "  " + m.snap.ShortID + " " + m.snap.Title + "  " +
		styleDim.Render(fmt.Sprintf("%s · Question %d", m.snap.State, max(n, 1)))
}

// focus is the large part on top: the agent's streaming text, the open
// Question, the summary, or how to go on.
func (m GrillScreen) focus(w int) []string {
	q := m.snap.open()
	switch {
	case m.running():
		head := "Claude is asking…"
		switch m.turn.Kind {
		case TurnSummary:
			head = "Claude is summing up…"
		case TurnWritePlan:
			head = "Claude is writing plan.md…"
		}
		lines := []string{styleWarn.Render(head)}
		if m.tool != "" {
			lines = append(lines, styleDim.Render("› "+oneLine(m.tool)))
		}
		return append(lines, wrap(m.stream, w)...)
	case m.phase == phaseEnded:
		return append([]string{styleTitle.Render("Summary")}, wrap(m.summary, w)...)
	case m.phase == phaseError && strings.TrimSpace(m.stream) != "":
		return append([]string{styleTitle.Render("Claude wrote")}, wrap(m.stream, w)...)
	case m.phase == phaseDone:
		lines := []string{styleTitle.Render("Nothing left to ask")}
		lines = append(lines, wrap(m.stream, w)...)
		return append(lines, "", "f finishes the Grill with a summary; Enter asks for another Question.")
	case q != nil:
		return append([]string{styleTitle.Render("Question")}, wrap(q.Text, w)...)
	case len(m.snap.Questions) == 0:
		return []string{"No Questions yet. Enter starts the Grill: Claude Code reads the Intent", "and asks one Question at a time; your answers are stored on the plan."}
	default:
		return []string{"No open Question. Enter continues the Grill; f finishes it."}
	}
}

// history lists the answered Questions, one line each with the answer
// under it, and the Messages of others.
func (m GrillScreen) history(w int) []string {
	var out []string
	n := 0
	for _, q := range m.snap.Questions {
		if q.Open {
			continue
		}
		n++
		out = append(out, truncate(fmt.Sprintf("%d. %s", n, oneLine(q.Text)), w),
			truncate(styleOK.Render("   → "+oneLine(q.Answer)), w))
	}
	for _, c := range m.snap.Others {
		out = append(out, truncate(styleWarn.Render(c.Who+": ")+oneLine(c.Text), w))
	}
	return out
}

func (m GrillScreen) footer() []string {
	var lines []string
	if m.notice != "" {
		style := styleWarn
		if m.failed {
			style = styleError
		}
		lines = append(lines, style.Render(m.notice))
	}
	if m.confirm {
		lines = append(lines, "plan.md is already written. Replace it with the agent's version? "+styleDim.Render("(y: yes, n: no)"))
	}
	switch m.phase {
	case phaseWaiting:
		lines = append(lines, styleAccent.Render("› ")+m.input+"▌")
	case phaseSaving:
		lines = append(lines, styleAccent.Render("› ")+m.input+styleDim.Render("  (saving…)"))
	}
	return append(lines, m.statusBar(), m.help.ShortHelpView(m.helpKeys()))
}

var grillSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m GrillScreen) statusBar() string {
	var s string
	switch m.phase {
	case phaseThinking:
		s = grillSpinner[m.frame%len(grillSpinner)] + " agent is thinking"
	case phaseWriting:
		s = grillSpinner[m.frame%len(grillSpinner)] + " writing plan.md"
	case phaseWaiting, phaseSaving:
		s = "▸ your answer"
	case phaseError:
		s = "✗ error"
	case phaseEnded:
		s = "✓ Grill finished"
	case phaseDone:
		s = "✓ nothing left to ask"
	default:
		s = "idle"
	}
	left := " " + s + " · author: " + orDash(m.snap.Author)
	right := " " + m.syncText + " "
	left = truncate(left, max(m.width-lipgloss.Width(right), 1))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 0)
	return styleSelected.Render(truncate(left+strings.Repeat(" ", gap)+right, m.width))
}

// helpKeys lists the keys valid now.
func (m GrillScreen) helpKeys() []key.Binding {
	k := m.keys
	switch {
	case m.confirm:
		return []key.Binding{k.Yes, k.No}
	case m.running():
		return []key.Binding{k.CancelTurn, k.Quit}
	case m.phase == phaseSaving:
		return []key.Binding{k.Quit}
	case m.phase == phaseWaiting:
		return []key.Binding{k.SendAnswer, k.FinishTyping, k.Cancel, k.Quit}
	case m.phase == phaseEnded:
		return []key.Binding{k.Write, k.Continue, k.Cancel, k.Quit}
	case m.phase == phaseDone:
		return []key.Binding{k.Finish, k.Continue, k.Cancel, k.Quit}
	case m.phase == phaseError && len(m.snap.Questions) == 0:
		return []key.Binding{k.Retry, k.Cancel, k.Quit}
	case m.phase == phaseError:
		return []key.Binding{k.Retry, k.Finish, k.Cancel, k.Quit}
	case len(m.snap.Questions) == 0:
		return []key.Binding{k.Start, k.Cancel, k.Quit}
	}
	return []key.Binding{k.Continue, k.Finish, k.Cancel, k.Quit}
}
