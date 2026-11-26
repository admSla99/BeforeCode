package tui

import (
	"context"
	"fmt"
	"slices"
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
	TurnAsk       GrillTurnKind = iota // ask the next round of Questions
	TurnSummary                        // finish the Grill with a summary
	TurnWritePlan                      // write plan.md
)

// GrillTurn is one turn of the agent.
type GrillTurn struct {
	Kind GrillTurnKind
	// Round is the round a TurnAsk asks, from 1 (ADR 0017).
	Round int
	// Answers are the author's answers to the round before, for a TurnAsk
	// that follows one; none to continue without them.
	Answers []GrillAnswer
	// Force lets a TurnWritePlan replace a written plan.md: the author
	// said yes.
	Force bool
}

// GrillAnswer is a Question of the Grill and the author's answer to it.
type GrillAnswer struct{ Question, Answer string }

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
	// the order they were asked, each with its round.
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
	Round  int // the round it was asked in, from 1 (core.GrillRounds)
	// Options are the likely answers the agent gave, and Recommended the
	// one it recommends; none when it gave none.
	Options     []string
	Recommended string
}

// customAnswer is the last item of a Question's options: the author writes
// the answer.
const customAnswer = "Write my own answer"

// GrillMessage is a Message outside the Grill's Questions and their
// answers, such as a comment by someone else.
type GrillMessage struct{ Who, Text string }

// open returns the Question the author answers now: the first open one of
// the last round, else the oldest open one; nil when none is open.
func (s GrillSnapshot) open() *GrillQuestion {
	last := s.lastRound()
	for i := range s.Questions {
		if s.Questions[i].Open && s.Questions[i].Round == last {
			return &s.Questions[i]
		}
	}
	for i := range s.Questions {
		if s.Questions[i].Open {
			return &s.Questions[i]
		}
	}
	return nil
}

// lastRound is the round of the newest Question, 0 before the first.
func (s GrillSnapshot) lastRound() int {
	last := 0
	for _, q := range s.Questions {
		last = max(last, q.Round)
	}
	return last
}

// round returns the Questions of round r.
func (s GrillSnapshot) round(r int) []GrillQuestion {
	var out []GrillQuestion
	for _, q := range s.Questions {
		if q.Round == r {
			out = append(out, q)
		}
	}
	return out
}

// limitReached reports whether the last of core.MaxGrillRounds rounds is
// asked and answered: no new round is asked.
func (s GrillSnapshot) limitReached() bool {
	return s.lastRound() >= core.MaxGrillRounds && s.open() == nil
}

// answers returns the answered Questions of round r with their answers.
func (s GrillSnapshot) answers(r int) []GrillAnswer {
	var out []GrillAnswer
	for _, q := range s.round(r) {
		if !q.Open {
			out = append(out, GrillAnswer{Question: q.Text, Answer: q.Answer})
		}
	}
	return out
}

// withAnswer returns s with the Question of this ID answered by text, as
// stored; the next snapshot confirms it.
func (s GrillSnapshot) withAnswer(id, text string) GrillSnapshot {
	s.Questions = slices.Clone(s.Questions)
	for i, q := range s.Questions {
		if q.ID == id {
			s.Questions[i].Open, s.Questions[i].Answer = false, text
		}
	}
	return s
}

// grillPhase is where the Grill stands (docs/design.md, section 3).
type grillPhase int

const (
	phaseIdle        grillPhase = iota // nothing runs; Enter starts or continues
	phaseThinking                      // a turn runs and streams
	phaseWaiting                       // the author types the answer
	phaseSaving                        // the answer is being stored
	phaseError                         // the last turn failed; Enter retries it
	phaseEnded                         // the agent wrote its summary
	phaseWriting                       // the write-plan turn runs
	phaseNothingLeft                   // the agent asked nothing: nothing is left to ask
	phaseLimit                         // the last round is answered: no new round is asked
)

// GrillScreen is the Grill of one plan: the Questions of a round one at a
// time, with the answered ones below them (ADR 0017). It writes through Grill only; the app syncs
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
	// choiceFor is the Question the option list was set up for; cursor is
	// the selected item, len(Options) for the custom answer, and typing is
	// set while the answer line is open.
	choiceFor string
	cursor    int
	typing    bool
	cancel    context.CancelFunc
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

type grillAnsweredMsg struct {
	id, text string // the Question and its answer
	err      error
}

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
			if !m.typing {
				m.input = ""
			}
			return m, nil
		}
		return m.answered(msg)
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

// answered goes on after an answer was stored: to the next open Question
// of the round, else to the next round, unless it was the last.
func (m GrillScreen) answered(msg grillAnsweredMsg) (tea.Model, tea.Cmd) {
	m.snap, m.input = m.snap.withAnswer(msg.id, msg.text), ""
	switch {
	case m.snap.open() != nil:
		m.phase = phaseWaiting
		m.setChoice()
	case m.snap.limitReached():
		m.phase = phaseLimit
	default:
		round := m.snap.lastRound()
		m, cmd := m.start(GrillTurn{Kind: TurnAsk, Round: round + 1, Answers: m.snap.answers(round)})
		return m, tea.Batch(cmd, m.reload(), grillChanged)
	}
	return m, tea.Batch(m.reload(), grillChanged)
}

// nextTurn is the turn that asks the next round.
func (m GrillScreen) nextTurn() GrillTurn {
	return GrillTurn{Kind: TurnAsk, Round: m.snap.lastRound() + 1}
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
		if m.snap.limitReached() {
			m.phase = phaseLimit
		}
		m.notice = strings.TrimSpace(m.notice + " The open Question was answered elsewhere.")
		m.failed = false
	case m.phase == phaseIdle && open != nil && msg.n == 0:
		m.phase = phaseWaiting // a Question from an earlier session waits
	case m.phase == phaseIdle && m.snap.limitReached():
		m.phase = phaseLimit
	case m.phase == phaseLimit && open != nil:
		m.phase = phaseWaiting // a Question that sync brought waits
	case m.phase == phaseNothingLeft && open != nil:
		m.phase = phaseWaiting // a Question that sync brought waits
	}
	m.setChoice()
	return m, nil
}

// setChoice sets up the option list when a new Question waits: the
// recommended option selected, or the first; the answer line alone for a
// Question without options.
func (m *GrillScreen) setChoice() {
	q := m.snap.open()
	if m.phase != phaseWaiting || q == nil || q.ID == m.choiceFor {
		return
	}
	m.choiceFor, m.cursor, m.typing, m.input = q.ID, 0, len(q.Options) == 0, ""
	for i, o := range q.Options {
		if o == q.Recommended {
			m.cursor = i
		}
	}
}

// choosing reports whether the author picks from the option list now.
func (m GrillScreen) choosing() bool { return m.phase == phaseWaiting && !m.typing }

// hasOptions reports whether the open Question has options.
func (m GrillScreen) hasOptions() bool {
	q := m.snap.open()
	return q != nil && len(q.Options) > 0
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
		m.phase = phaseNothingLeft
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
	case m.phase == phaseWaiting && m.typing && m.hasOptions() && key.Matches(msg, m.keys.BackToOptions):
		m.typing = false
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		return m, func() tea.Msg { return closeGrillMsg{} }
	case m.choosing():
		return m.pick(msg)
	case m.phase == phaseWaiting:
		switch {
		case key.Matches(msg, m.keys.FinishTyping):
			return m.start(GrillTurn{Kind: TurnSummary})
		case key.Matches(msg, m.keys.Send):
			if strings.TrimSpace(m.input) == "" {
				return m, nil
			}
			return m.answer(strings.TrimSpace(m.input))
		}
		editLine(&m.input, msg)
		return m, nil
	case key.Matches(msg, m.keys.Send) && m.phase == phaseError:
		return m.start(m.turn) // retry the turn that failed
	case key.Matches(msg, m.keys.Send) && m.phase == phaseLimit:
		return m, nil // no round after the last
	case key.Matches(msg, m.keys.Send):
		return m.start(m.nextTurn())
	case key.Matches(msg, m.keys.Finish) && m.phase != phaseEnded && (len(m.snap.Questions) > 0 || m.phase == phaseNothingLeft):
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

// pick handles a key while the author picks from the option list: ↑↓
// select, enter answers with the option or opens the answer line for the
// custom item.
func (m GrillScreen) pick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := m.snap.open()
	switch {
	case key.Matches(msg, m.keys.Up):
		m.cursor = max(m.cursor-1, 0)
	case key.Matches(msg, m.keys.Down):
		m.cursor = min(m.cursor+1, len(q.Options))
	case key.Matches(msg, m.keys.Finish):
		return m.start(GrillTurn{Kind: TurnSummary})
	case key.Matches(msg, m.keys.Send) && m.cursor == len(q.Options):
		m.typing = true
	case key.Matches(msg, m.keys.Send):
		return m.answer(q.Options[m.cursor])
	}
	return m, nil
}

// answer stores text as the author's answer to the open Question.
func (m GrillScreen) answer(text string) (tea.Model, tea.Cmd) {
	q, g, short := m.snap.open(), m.grill, m.shortID
	m.input, m.phase = text, phaseSaving
	id := q.ID
	return m, func() tea.Msg {
		return grillAnsweredMsg{id: id, text: text, err: g.Answer(context.Background(), short, id, text)}
	}
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
	limit := core.MaxGrillRounds
	q := m.snap.open()
	var count string
	switch {
	case m.phase == phaseThinking && m.turn.Kind == TurnAsk && q == nil:
		count = fmt.Sprintf("round %d/%d", max(m.turn.Round, 1), limit) // the round being asked
	case q != nil:
		round := m.snap.round(q.Round)
		i := slices.IndexFunc(round, func(o GrillQuestion) bool { return o.ID == q.ID })
		count = fmt.Sprintf("round %d/%d · Q%d/%d", max(q.Round, 1), limit, i+1, len(round))
	case m.phase == phaseNothingLeft:
		count = plural(len(m.snap.Questions), "Question") + " asked"
	default:
		count = fmt.Sprintf("round %d/%d", max(m.snap.lastRound(), 1), limit)
	}
	return styleTitle.Render("Grill") + "  " + m.snap.ShortID + " " + m.snap.Title + "  " +
		styleDim.Render(string(m.snap.State)+" · "+count)
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
	case m.phase == phaseNothingLeft:
		lines := []string{styleTitle.Render("Nothing left to ask")}
		lines = append(lines, wrap(m.stream, w)...)
		return append(lines, "", "f finishes the Grill with a summary; Enter asks for another round.")
	case m.phase == phaseLimit:
		return []string{
			styleTitle.Render("Round limit reached"),
			fmt.Sprintf("All %d rounds are asked and answered. f finishes the Grill with a summary.", core.MaxGrillRounds),
		}
	case q != nil:
		lines := append([]string{styleTitle.Render("Question")}, wrap(q.Text, w)...)
		if len(q.Options) == 0 {
			return lines
		}
		lines = append(lines, "")
		for i, o := range append(slices.Clone(q.Options), customAnswer) {
			// The option is cut, not the mark after it.
			if i < len(q.Options) && q.Options[i] == q.Recommended {
				const rec = " (recommended)"
				o = truncate(oneLine(o), max(w-2-len(rec), 1)) + rec
			} else {
				o = oneLine(o)
			}
			mark := "  "
			if m.choosing() && i == m.cursor {
				mark, o = styleAccent.Render("› "), styleAccent.Render(o)
			}
			lines = append(lines, mark+o)
		}
		return lines
	case len(m.snap.Questions) == 0:
		return []string{"No Questions yet. Enter starts the Grill: Claude Code reads the Intent", fmt.Sprintf("and asks in rounds, at most %d; your answers are stored on the plan.", core.MaxGrillRounds)}
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
	switch {
	case m.phase == phaseWaiting && m.typing:
		lines = append(lines, styleAccent.Render("› ")+m.input+"▌")
	case m.phase == phaseSaving:
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
	case phaseNothingLeft:
		s = "✓ nothing left to ask"
	case phaseLimit:
		s = fmt.Sprintf("✓ %d rounds asked", core.MaxGrillRounds)
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
	case m.choosing():
		return []key.Binding{k.Up, k.SendAnswer, k.Finish, k.Cancel, k.Quit}
	case m.phase == phaseWaiting && m.hasOptions():
		return []key.Binding{k.SendAnswer, k.FinishTyping, k.BackToOptions, k.Quit}
	case m.phase == phaseWaiting:
		return []key.Binding{k.SendAnswer, k.FinishTyping, k.Cancel, k.Quit}
	case m.phase == phaseEnded:
		return []key.Binding{k.Write, k.Continue, k.Cancel, k.Quit}
	case m.phase == phaseNothingLeft:
		return []key.Binding{k.Finish, k.Continue, k.Cancel, k.Quit}
	case m.phase == phaseLimit:
		return []key.Binding{k.Finish, k.Cancel, k.Quit}
	case m.phase == phaseError && len(m.snap.Questions) == 0:
		return []key.Binding{k.Retry, k.Cancel, k.Quit}
	case m.phase == phaseError:
		return []key.Binding{k.Retry, k.Finish, k.Cancel, k.Quit}
	case len(m.snap.Questions) == 0:
		return []key.Binding{k.Start, k.Cancel, k.Quit}
	}
	return []key.Binding{k.Continue, k.Finish, k.Cancel, k.Quit}
}
