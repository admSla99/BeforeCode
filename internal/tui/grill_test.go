package tui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// fakeGrill is the test adapter of Grill: an in-memory Grill whose agent
// follows a script.
type fakeGrill struct {
	mu      sync.Mutex
	snap    GrillSnapshot
	turns   []GrillTurn
	answers []string
	// script returns what the agent of a turn writes; ask, when set, is
	// the Question it records before the turn ends.
	script func(t GrillTurn) (text string, ask string, err error)
	// hold, when set, keeps every turn running until it is closed.
	hold chan struct{}
	// tool, when set, is a command the agent runs in every turn.
	tool string
	n    int
}

func newFakeGrill() *fakeGrill {
	return &fakeGrill{
		snap: GrillSnapshot{ShortID: "bfc-5v7x", Title: "Payments via Stripe", State: core.StateDraft,
			Author: "Jana Nováková", AuthorEmail: "jana@example.com"},
		script: func(t GrillTurn) (string, string, error) {
			switch t.Kind {
			case TurnSummary:
				return "Summary: success is a test-card checkout. Next: write plan.md.", "", nil
			case TurnWritePlan:
				return "Wrote plan.md.", "", nil
			}
			return "", "", nil
		},
	}
}

// questions are what the scripted agent asks, in order.
var questions = []string{
	"What observable result would prove it worked?",
	"What happens when a card payment fails?",
	"Is stock held while a payment is pending?",
}

// asking makes the agent ask the next of questions in every Grill turn.
func (f *fakeGrill) asking() *fakeGrill {
	base := f.script
	f.script = func(t GrillTurn) (string, string, error) {
		if t.Kind != TurnAsk {
			return base(t)
		}
		q := questions[f.askedCount()%len(questions)]
		return q, q, nil
	}
	return f
}

// askedCount is the number of Questions asked so far; mu is held.
func (f *fakeGrill) askedCount() int { return len(f.snap.Questions) }

func (f *fakeGrill) Snapshot(context.Context, string) (GrillSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.snap
	s.Questions = slices.Clone(s.Questions)
	s.Others = slices.Clone(s.Others)
	return s, nil
}

func (f *fakeGrill) Turn(ctx context.Context, _ string, t GrillTurn) (<-chan GrillEvent, error) {
	f.mu.Lock()
	f.turns = append(f.turns, t)
	text, ask, err := f.script(t)
	hold, tool := f.hold, f.tool
	f.mu.Unlock()
	ch := make(chan GrillEvent)
	go func() {
		defer close(ch)
		send := func(e GrillEvent) bool {
			select {
			case ch <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}
		// The text streams in two pieces.
		half := len(text) / 2
		if !send(GrillEvent{Text: text[:half]}) {
			return
		}
		if tool != "" && !send(GrillEvent{Tool: tool}) {
			return
		}
		if ask != "" {
			f.mu.Lock()
			f.n++
			f.snap.Questions = append(f.snap.Questions, GrillQuestion{ID: "Q" + string(rune('0'+f.n)), Text: ask, Open: true})
			f.mu.Unlock()
		}
		if hold != nil {
			select {
			case <-hold:
			case <-ctx.Done():
				ch <- GrillEvent{Err: ctx.Err()}
				return
			}
		}
		if !send(GrillEvent{Text: text[half:]}) {
			return
		}
		if err != nil {
			send(GrillEvent{Err: err})
			return
		}
		if t.Kind == TurnWritePlan {
			f.mu.Lock()
			f.snap.PlanWritten = true
			f.mu.Unlock()
		}
	}()
	return ch, nil
}

func (f *fakeGrill) Answer(_ context.Context, _ string, questionID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, questionID+" "+text)
	for i, q := range f.snap.Questions {
		if q.ID == questionID {
			f.snap.Questions[i].Open, f.snap.Questions[i].Answer = false, text
		}
	}
	return nil
}

func (f *fakeGrill) recordedTurns() []GrillTurn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.turns)
}

// newGrillModel starts the Grill screen of bfc-5v7x in an 80x24 terminal.
func newGrillModel(t *testing.T, g *fakeGrill) *teatest.TestModel {
	t.Helper()
	return newTestModel(t, NewGrill(g, "bfc-5v7x"), 80, 24)
}

func typeText(tm *teatest.TestModel, s string) {
	for _, r := range s {
		tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func pressKey(tm *teatest.TestModel, code rune) {
	tm.Send(tea.KeyPressMsg{Code: code, Text: printable(code)})
}

func printable(r rune) string {
	if r >= 32 && r < 127 {
		return string(r)
	}
	return ""
}

// finalGolden quits and compares the final view with the golden file.
func finalGolden(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	out := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(GrillScreen).View().Content
	golden(t, ansi.Strip(out))
}

// AC-2: before the first turn the screen says how to start; the header
// names the plan, the status bar the author.
func TestGrillBeforeTheFirstTurn(t *testing.T) {
	t.Parallel()
	tm := newGrillModel(t, newFakeGrill())
	waitFor(t, tm, "Enter starts the Grill")
	finalGolden(t, tm)
}

// AC-2, AC-3: a turn streams the agent's text, then the screen waits for
// the answer to the Question the agent recorded; the answer is stored and
// starts the next turn; answered Questions are listed below the open one.
func TestGrillAsksAndRecordsTheAnswer(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "send answer")
	typeText(tm, "A test-card checkout turns the order paid.")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "answered (1)")
	waitFor(t, tm, questions[1])
	waitFor(t, tm, "send answer")
	finalGolden(t, tm)

	g.mu.Lock()
	defer g.mu.Unlock()
	if want := []string{"Q1 A test-card checkout turns the order paid."}; !slices.Equal(g.answers, want) {
		t.Errorf("answers = %q, want %q", g.answers, want)
	}
	if len(g.turns) != 2 || g.turns[0].Kind != TurnAsk || g.turns[0].Answer != "" || g.turns[1].Answer != "A test-card checkout turns the order paid." {
		t.Errorf("turns = %+v", g.turns)
	}
}

// AC-3: a turn that ends without a recorded Question is an error; Enter
// retries it.
func TestGrillTurnWithoutAQuestionIsAnError(t *testing.T) {
	t.Parallel()
	g := newFakeGrill()
	g.script = func(GrillTurn) (string, string, error) { return "What is the problem?", "", nil }
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "without a recorded Question")
	finalGolden(t, tm)
}

// AC-4: a failed turn shows the reason, and Enter retries.
func TestGrillAgentErrorAndRetry(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	fail := true
	base := g.script
	g.script = func(turn GrillTurn) (string, string, error) {
		if fail {
			fail = false
			return "", "", errors.New("claude exited with status 1: rate limited")
		}
		return base(turn)
	}
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "rate limited")
	waitFor(t, tm, "enter retry")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "send answer")
	waitFor(t, tm, questions[0])
	if n := len(g.recordedTurns()); n != 2 {
		t.Errorf("%d turns, want 2", n)
	}
}

// AC-4: Esc during a turn stops it. Before the agent recorded its Question
// the screen is idle; after, it waits for the answer.
func TestGrillEscCancelsTheTurn(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	g.hold = make(chan struct{}) // turns run until cancelled
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "agent is thinking")
	// The fake agent records its Question as soon as the turn starts.
	pressKey(tm, tea.KeyEscape)
	waitFor(t, tm, "Turn cancelled")
	waitFor(t, tm, "send answer")
	waitFor(t, tm, questions[0])
	finalGolden(t, tm)
}

// AC-4: Esc in any other state asks the app to return to the plan detail.
func TestGrillEscLeaves(t *testing.T) {
	t.Parallel()
	m := NewGrill(newFakeGrill(), "bfc-5v7x")
	next, _ := m.Update(m.load())
	_, cmd := next.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("esc does nothing")
	}
	if _, ok := cmd().(closeGrillMsg); !ok {
		t.Errorf("esc sends %T, want closeGrillMsg", cmd())
	}
}

// AC-6: when sync brings an answer to the open Question from elsewhere, the
// answer line closes and Enter continues; Messages from others appear.
func TestGrillSyncBringsAnAnswerFromElsewhere(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "send answer")
	g.mu.Lock()
	g.snap.Questions[0].Open, g.snap.Questions[0].Answer = false, "Answered in the CLI."
	g.snap.Others = append(g.snap.Others, GrillComment{Who: "Peter Horváth", Text: "Should refunds stay out?"})
	g.mu.Unlock()
	tm.Send(GrillReloadMsg{})
	waitFor(t, tm, "answered elsewhere")
	waitFor(t, tm, "Should refunds stay out?")
	finalGolden(t, tm)
}

// AC-7: f asks for the summary; w writes plan.md and reports the edit.
func TestGrillFinishAndWritePlan(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "send answer")
	tm.Send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	waitFor(t, tm, "Grill finished")
	waitFor(t, tm, "Summary: success")
	waitFor(t, tm, "w write plan.md")
	pressKey(tm, 'w')
	waitFor(t, tm, "writing plan.md")
	deadline := time.Now().Add(5 * time.Second)
	for len(g.recordedTurns()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	turns := g.recordedTurns()
	if len(turns) < 3 {
		t.Fatalf("turns = %+v", turns)
	}
	if kinds := []GrillTurnKind{turns[0].Kind, turns[1].Kind, turns[2].Kind}; !slices.Equal(kinds, []GrillTurnKind{TurnAsk, TurnSummary, TurnWritePlan}) || turns[2].Force {
		t.Errorf("turns = %+v", turns)
	}
}

// AC-7: when plan.md is already written the screen asks first; the yes
// lets the agent replace it.
func TestGrillAsksBeforeReplacingPlanMd(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	g.snap.PlanWritten = true
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "send answer")
	tm.Send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	waitFor(t, tm, "Grill finished")
	pressKey(tm, 'w')
	waitFor(t, tm, "plan.md is already written")
	finalGolden(t, tm)
}

// openGrill starts the app with a Grill, opens the plan bfc-5v7x and
// presses g.
func openGrill(t *testing.T, f *fakeTeam, g *fakeGrill) *teatest.TestModel {
	t.Helper()
	app := NewApp(f, f, AppOptions{Interval: time.Hour, Grill: g})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "IN-REVIEW")
	send(tm, "enter")
	waitFor(t, tm, "g grill")
	send(tm, "g")
	waitFor(t, tm, "Enter starts the Grill")
	return tm
}

// AC-1: g on the plan detail opens the Grill; esc returns to the detail.
func TestAppGrillOpensFromTheDetailAndEscReturns(t *testing.T) {
	f := newFakeTeam()
	tm := openGrill(t, f, newFakeGrill())
	send(tm, "esc")
	waitFor(t, tm, "Affected areas")
	if out := screen(t, tm); !strings.Contains(out, "bfc-5v7x") || strings.Contains(out, "Enter starts the Grill") {
		t.Errorf("not back on the detail:\n%s", out)
	}
}

// AC-1: a closed plan offers no Grill.
func TestAppNoGrillForAClosedPlan(t *testing.T) {
	f := newFakeTeam()
	d := f.details["bfc-5v7x"]
	d.State, d.Editable, d.Actions = core.StateClosed, false, nil
	f.details["bfc-5v7x"] = d
	app := NewApp(f, f, AppOptions{Interval: time.Hour, Grill: newFakeGrill()})
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "IN-REVIEW")
	send(tm, "enter")
	waitFor(t, tm, "Affected areas")
	send(tm, "g")
	if out := screen(t, tm); strings.Contains(out, "g grill") || strings.Contains(out, "Enter starts the Grill") {
		t.Errorf("a closed plan offers the Grill:\n%s", out)
	}
}

// AC-6, AC-7: every answer is a local change the app syncs after; writing
// plan.md returns to the detail on plan.md and syncs.
func TestAppGrillSyncsAndReturnsOnPlanMd(t *testing.T) {
	f := newFakeTeam()
	g := newFakeGrill().asking()
	tm := openGrill(t, f, g)
	before := f.syncCount()
	send(tm, "enter")
	waitFor(t, tm, "send answer")
	typeText(tm, "Paid without anyone touching it.")
	send(tm, "enter")
	waitFor(t, tm, "answered (1)")
	deadline := time.Now().Add(5 * time.Second)
	for f.syncCount() <= before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.syncCount() <= before {
		t.Error("no sync after the answer")
	}
	waitFor(t, tm, "send answer")
	tm.Send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	waitFor(t, tm, "w write plan.md")
	send(tm, "w")
	waitFor(t, tm, "Affected areas") // the detail, on plan.md
	if out := screen(t, tm); strings.Contains(out, "Grill finished") {
		t.Errorf("still on the Grill:\n%s", out)
	}
}

// AC-6: a sync that brings changes reloads the Grill.
func TestAppSyncReloadsTheGrill(t *testing.T) {
	f := newFakeTeam()
	g := newFakeGrill()
	tm := openGrill(t, f, g)
	g.mu.Lock()
	g.snap.Others = append(g.snap.Others, GrillComment{Who: "Peter Horváth", Text: "Refunds?"})
	g.mu.Unlock()
	f.mu.Lock()
	f.syncFn = func() SyncResult { return SyncResult{Outcome: SyncOK, Changed: true} }
	f.mu.Unlock()
	tm.Send(syncTickMsg{})
	waitFor(t, tm, "Peter Horváth: Refunds?")
	_ = screen(t, tm)
}

// While the agent runs a command, the screen shows it, so a long turn
// without text still shows progress.
func TestGrillShowsTheAgentsCommand(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().asking()
	g.hold = make(chan struct{})
	g.tool = "bfc ask bfc-5v7x --to jana@example.com"
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "› bfc ask bfc-5v7x --to jana@example.com")
	close(g.hold)
	waitFor(t, tm, "send answer")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
}
