package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// askingRounds makes the agent ask a round of three Questions in every
// Grill turn: "R<round> Q<i>?".
func (f *fakeGrill) askingRounds() *fakeGrill {
	base := f.script
	f.script = func(t GrillTurn) (string, string, error) {
		if t.Kind != TurnAsk {
			return base(t)
		}
		var qs []string
		for i := 1; i <= 3; i++ {
			qs = append(qs, fmt.Sprintf("R%d Q%d?", t.Round, i))
		}
		return "Three Questions.", strings.Join(qs, "\x00"), nil
	}
	return f
}

// answerLine types an answer and sends it.
func answerLine(tm interface{ Send(tea.Msg) }, text string) {
	for _, r := range text {
		tm.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	tm.Send(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// bfc-121s AC-12: one turn asks a round of three; each answer is stored at
// once and the next Question of the round follows without a turn.
func TestGrillTakesARoundOneQuestionAtATime(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().askingRounds()
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "round 1/3 · Q1/3")
	waitFor(t, tm, "R1 Q1?")
	answerLine(tm, "One.")
	waitFor(t, tm, "round 1/3 · Q2/3")
	waitFor(t, tm, "R1 Q2?")
	if n := len(g.recordedTurns()); n != 1 {
		t.Errorf("%d turns after the first answer, want 1", n)
	}
	if got := g.recordedAnswers(); len(got) != 1 || got[0] != "Q1 One." {
		t.Errorf("answers = %q", got)
	}
	finalGolden(t, tm)
}

// bfc-121s AC-12: once the round is answered, the next turn asks round 2
// with all three answers.
func TestGrillStartsTheNextRoundWithItsAnswers(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().askingRounds()
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Enter starts the Grill")
	pressKey(tm, tea.KeyEnter)
	for i, a := range []string{"One.", "Two.", "Three."} {
		waitFor(t, tm, fmt.Sprintf("round 1/3 · Q%d/3", i+1))
		answerLine(tm, a)
	}
	waitFor(t, tm, "round 2/3 · Q1/3")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	turns := g.recordedTurns()
	want := []GrillAnswer{{"R1 Q1?", "One."}, {"R1 Q2?", "Two."}, {"R1 Q3?", "Three."}}
	if len(turns) != 2 || turns[1].Round != 2 || fmt.Sprint(turns[1].Answers) != fmt.Sprint(want) {
		t.Errorf("turns = %+v", turns)
	}
}

// bfc-121s AC-13: after round 3 is answered no turn starts; the screen
// says the limit is reached, f sums up and w writes plan.md.
func TestGrillStopsAfterTheLastRound(t *testing.T) {
	t.Parallel()
	g := newFakeGrill()
	for r := 1; r <= 3; r++ {
		g.withOpenQuestion(fmt.Sprintf("R%d?", r), "")
		g.snap.Questions[r-1].Round = r
		if r < 3 {
			g.snap.Questions[r-1].Open, g.snap.Questions[r-1].Answer = false, "Yes."
		}
	}
	tm := newGrillModel(t, g)
	waitFor(t, tm, "round 3/3 · Q1/1")
	answerLine(tm, "Done.")
	waitFor(t, tm, "Round limit reached")
	waitFor(t, tm, "f finish")
	pressKey(tm, tea.KeyEnter) // starts no round
	finalGolden(t, tm)
	if turns := g.recordedTurns(); len(turns) != 0 {
		t.Errorf("turns = %+v, want none", turns)
	}

	tm = newGrillModel(t, g)
	waitFor(t, tm, "Round limit reached")
	pressKey(tm, 'f')
	waitFor(t, tm, "Summary: success")
	pressKey(tm, 'w')
	waitFor(t, tm, "writing plan.md")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	if turns := g.recordedTurns(); len(turns) < 1 || turns[0].Kind != TurnSummary {
		t.Errorf("turns = %+v", turns)
	}
}

// bfc-121s AC-14: opening the Grill again in the middle of a round shows
// its next open Question; one answered in another clone drops out on sync.
func TestGrillResumesARoundAndDropsAnsweredElsewhere(t *testing.T) {
	t.Parallel()
	g := newFakeGrill()
	g.withOpenQuestion("Q one?", "").withOpenQuestion("Q two?", "").withOpenQuestion("Q three?", "")
	g.snap.Questions[0].Open, g.snap.Questions[0].Answer = false, "Stored before esc."
	tm := newGrillModel(t, g)
	waitFor(t, tm, "round 1/3 · Q2/3")
	waitFor(t, tm, "Q two?")

	g.mu.Lock()
	g.snap.Questions[1].Open, g.snap.Questions[1].Answer = false, "From the other clone."
	g.mu.Unlock()
	tm.Send(reloadGrillMsg{})
	waitFor(t, tm, "round 1/3 · Q3/3")
	waitFor(t, tm, "Q three?")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	if turns := g.recordedTurns(); len(turns) != 0 {
		t.Errorf("turns = %+v, want none", turns)
	}
}
