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
	"github.com/charmbracelet/x/ansi"

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
	Others []GrillComment
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

// GrillComment is a Message outside the Grill's Questions and answers.
type GrillComment struct{ Who, Text string }

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
	phaseError                      // the last turn failed; Enter retries
	phaseEnded                      // the agent wrote its summary
	phaseWriting                    // the write-plan turn runs
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
	// stopped is set when the author cancelled the running turn.
	stopped bool
	// settle is set when the next Snapshot decides the phase after a turn.
	settle   bool
	frame    int    // of the spinner
	syncText string // set by the app before each View
}

// NewGrill returns the Grill screen of the plan with this Short ID.
func NewGrill(grill Grill, shortID string) GrillScreen {
	return GrillScreen{grill: grill, shortID: shortID, keys: newGrillKeys(), help: help.New(), width: 80, height: 24}
}

// GrillChangedMsg reports that the Grill wrote to the plan: a Question or an
// answer. The app syncs after it.
type GrillChangedMsg struct{}

// GrillReloadMsg asks the screen to read the plan's Grill again, after a
// sync brought changes.
type GrillReloadMsg struct{}

// closeGrillMsg asks the app to return to the plan detail.
type closeGrillMsg struct{}

type grillLoadedMsg struct {
	snap GrillSnapshot
	err  error
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

func (m GrillScreen) load() tea.Msg {
	s, err := m.grill.Snapshot(context.Background(), m.shortID)
	return grillLoadedMsg{s, err}
}

// Init loads the plan's Grill.
func (m GrillScreen) Init() tea.Cmd { return m.load }

func next(ch <-chan GrillEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		return grillEventMsg{ev: ev, ch: ch, done: !ok}
	}
}

func tickSpinner() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return grillTickMsg{} })
}

// start runs a turn.
func (m GrillScreen) start(t GrillTurn) (GrillScreen, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel, m.turn, m.stream, m.tool, m.stopped, m.confirm = cancel, t, "", "", false, false
	m.phase = phaseThinking
	if t.Kind == TurnWritePlan {
		m.phase = phaseWriting
	}
	m.notice, m.failed = "", false
	g, short := m.grill, m.shortID
	return m, tea.Batch(func() tea.Msg {
		ch, err := g.Turn(ctx, short, t)
		return grillStartedMsg{ch, err}
	}, tickSpinner())
}

// Update handles a message.
func (m GrillScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
	case grillLoadedMsg:
		return m.loadedSnap(msg)
	case GrillReloadMsg:
		return m, m.load
	case grillStartedMsg:
		if msg.err != nil {
			return m.turnFailed(msg.err)
		}
		return m, next(msg.ch)
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
		return m, tea.Batch(cmd, m.load, changed)
	case grillTickMsg:
		if m.phase == phaseThinking || m.phase == phaseWriting {
			m.frame++
			return m, tickSpinner()
		}
	case tea.KeyPressMsg:
		return m.press(msg)
	}
	return m, nil
}

func changed() tea.Msg { return GrillChangedMsg{} }

// loadedSnap takes a new snapshot, and after a turn decides what comes next.
func (m GrillScreen) loadedSnap(msg grillLoadedMsg) (tea.Model, tea.Cmd) {
	m.loaded, m.loadErr = true, msg.err
	if msg.err != nil {
		return m, nil
	}
	before := m.snap
	m.snap = msg.snap
	if n := len(m.snap.Others) - len(before.Others); n > 0 && before.ShortID != "" {
		m.notice, m.failed = fmt.Sprintf("Sync brought %d new Message(s).", n), false
	}
	open := m.snap.open()
	switch {
	case m.settle:
		m.settle = false
		switch {
		case open != nil:
			m.phase = phaseWaiting
		case m.stopped:
			m.phase = phaseIdle
		default:
			m.phase, m.notice, m.failed = phaseError, "The turn ended without a recorded Question; the agent did not run bfc ask.", true
		}
	case m.phase == phaseWaiting && open == nil:
		m.phase, m.input = phaseIdle, ""
		m.notice = strings.TrimSpace(m.notice + " The open Question was answered elsewhere.")
		m.failed = false
	case m.phase == phaseIdle && open != nil && before.ShortID == "":
		m.phase = phaseWaiting // a Question from an earlier session waits
	}
	return m, nil
}

// event handles one event of the running turn.
func (m GrillScreen) event(msg grillEventMsg) (tea.Model, tea.Cmd) {
	if msg.done {
		return m.turnEnded()
	}
	if msg.ev.Err != nil {
		if m.stopped {
			return m, next(msg.ch) // the cancel; the turn ends next
		}
		return m.turnFailed(msg.ev.Err)
	}
	m.stream += msg.ev.Text
	if msg.ev.Tool != "" {
		m.tool = msg.ev.Tool
	}
	return m, next(msg.ch)
}

func (m GrillScreen) turnFailed(err error) (tea.Model, tea.Cmd) {
	m.phase, m.notice, m.failed = phaseError, err.Error(), true
	m.cancel = nil
	return m, tea.Batch(m.load, changed)
}

// turnEnded decides what comes after a turn that ran to its end or was
// cancelled.
func (m GrillScreen) turnEnded() (tea.Model, tea.Cmd) {
	if m.phase == phaseError {
		return m, nil // turnFailed handled it
	}
	m.cancel = nil
	switch {
	case m.stopped:
		m.notice, m.failed = "Turn cancelled.", false
		if m.turn.Kind != TurnAsk {
			m.phase = phaseIdle
			return m, nil
		}
		m.settle = true
		return m, tea.Batch(m.load, changed)
	case m.turn.Kind == TurnSummary:
		m.phase, m.summary = phaseEnded, strings.TrimSpace(m.stream)
		return m, nil
	case m.turn.Kind == TurnWritePlan:
		return m, func() tea.Msg { return PlanEditedMsg{Changed: true} }
	}
	m.settle = true
	return m, tea.Batch(m.load, changed)
}

func (m GrillScreen) press(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	running := m.phase == phaseThinking || m.phase == phaseWriting
	switch {
	case key.Matches(msg, m.keys.Quit):
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case m.confirm:
		m.confirm = false
		if key.Matches(msg, m.keys.Yes) {
			return m.start(GrillTurn{Kind: TurnWritePlan, Force: true})
		}
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		if running {
			m.stopped = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		return m, func() tea.Msg { return closeGrillMsg{} }
	case running || !m.loaded:
		return m, nil
	case m.phase == phaseWaiting:
		switch {
		case key.Matches(msg, m.keys.FinishTyping):
			return m.start(GrillTurn{Kind: TurnSummary})
		case key.Matches(msg, m.keys.Send):
			if strings.TrimSpace(m.input) == "" {
				return m, nil
			}
			q, g, short, text := m.snap.open(), m.grill, m.shortID, strings.TrimSpace(m.input)
			m.input = text
			return m, func() tea.Msg { return grillAnsweredMsg{g.Answer(context.Background(), short, q.ID, text)} }
		}
		editLine(&m.input, msg)
		return m, nil
	case key.Matches(msg, m.keys.Send):
		return m.start(GrillTurn{Kind: TurnAsk})
	case key.Matches(msg, m.keys.Finish) && m.phase != phaseEnded && len(m.snap.Questions) > 0:
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
	if !m.loaded {
		return tea.NewView("Loading " + m.shortID + "…")
	}
	if m.loadErr != nil {
		return tea.NewView(styleError.Render(m.loadErr.Error()) + "\n" + m.help.ShortHelpView([]key.Binding{m.keys.Cancel, m.keys.Quit}))
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
	if m.phase == phaseThinking && m.turn.Kind == TurnAsk {
		n++
	}
	return styleTitle.Render("Grill") + "  " + m.snap.ShortID + " " + m.snap.Title + "  " +
		styleDim.Render(fmt.Sprintf("%s · Question %d", m.snap.State, max(n, 1)))
}

// focus is the large part on top: the agent's streaming text, the open
// Question, the summary, or how to go on.
func (m GrillScreen) focus(w int) []string {
	q := m.snap.open()
	switch {
	case m.phase == phaseThinking || m.phase == phaseWriting:
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
	if m.phase == phaseWaiting {
		lines = append(lines, styleAccent.Render("› ")+m.input+"▌")
	}
	return append(lines, m.statusBar(), m.help.ShortHelpView(m.helpKeys()))
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m GrillScreen) statusBar() string {
	var s string
	switch m.phase {
	case phaseThinking:
		s = spinner[m.frame%len(spinner)] + " agent is thinking"
	case phaseWriting:
		s = spinner[m.frame%len(spinner)] + " writing plan.md"
	case phaseWaiting:
		s = "▸ your answer"
	case phaseError:
		s = "✗ error"
	case phaseEnded:
		s = "✓ Grill finished"
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
	case m.phase == phaseThinking || m.phase == phaseWriting:
		return []key.Binding{k.CancelTurn, k.Quit}
	case m.phase == phaseWaiting:
		return []key.Binding{k.SendAnswer, k.FinishTyping, k.Cancel, k.Quit}
	case m.phase == phaseEnded:
		return []key.Binding{k.Write, k.Continue, k.Cancel, k.Quit}
	case m.phase == phaseError && len(m.snap.Questions) == 0:
		return []key.Binding{k.Retry, k.Cancel, k.Quit}
	case m.phase == phaseError:
		return []key.Binding{k.Retry, k.Finish, k.Cancel, k.Quit}
	case len(m.snap.Questions) == 0:
		return []key.Binding{k.Start, k.Cancel, k.Quit}
	}
	return []key.Binding{k.Continue, k.Finish, k.Cancel, k.Quit}
}

// wrap wraps text to w cells.
func wrap(text string, w int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(strings.TrimSpace(text), max(w, 1), ""), "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func tail(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

func pad(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
