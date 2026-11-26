package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// withOpenQuestion gives the Grill an open Question, as from an earlier
// session, with these options and recommendation.
func (f *fakeGrill) withOpenQuestion(text, recommended string, options ...string) *fakeGrill {
	f.n++
	f.snap.Questions = append(f.snap.Questions, GrillQuestion{
		ID: "Q" + string(rune('0'+f.n)), Text: text, Open: true, Round: 1, Options: options, Recommended: recommended,
	})
	return f
}

func (f *fakeGrill) recordedAnswers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.answers)
}

// bfc-121s AC-5, AC-8: the options are a list with the recommended one
// marked and selected and the custom item last.
func TestGrillShowsOptions(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "EUR", "USD", "EUR")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› EUR (recommended)")
	waitFor(t, tm, "enter send answer")
	finalGolden(t, tm)
}

// bfc-121s AC-5: enter answers with the selected option, exactly its text.
func TestGrillEnterAnswersWithTheRecommendedOption(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "EUR", "USD", "EUR")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› EUR (recommended)")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "answered (1)")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	if got, want := g.recordedAnswers(), []string{"Q1 EUR"}; !slices.Equal(got, want) {
		t.Errorf("answers = %q, want %q", got, want)
	}
}

// bfc-121s AC-5: ↑ then enter answers with the option above.
func TestGrillUpPicksAnotherOption(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "EUR", "USD", "EUR")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› EUR (recommended)")
	pressKey(tm, tea.KeyUp)
	waitFor(t, tm, "› USD")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "answered (1)")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	if got, want := g.recordedAnswers(), []string{"Q1 USD"}; !slices.Equal(got, want) {
		t.Errorf("answers = %q, want %q", got, want)
	}
}

// bfc-121s AC-6: the custom item opens the answer line; esc goes back to
// the list without storing anything; the typed text is stored as before.
func TestGrillCustomAnswer(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "", "USD", "EUR")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› USD")
	pressKey(tm, tea.KeyDown)
	pressKey(tm, tea.KeyDown)
	waitFor(t, tm, "› Write my own answer")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "esc options")
	pressKey(tm, tea.KeyEscape)
	waitFor(t, tm, "↑↓ select")
	if got := g.recordedAnswers(); len(got) != 0 {
		t.Errorf("esc stored %q", got)
	}
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "esc options")
	typeText(tm, "CHF, and EUR later")
	waitFor(t, tm, "CHF, and EUR later▌")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "answered (1)")
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	if got, want := g.recordedAnswers(), []string{"Q1 CHF, and EUR later"}; !slices.Equal(got, want) {
		t.Errorf("answers = %q, want %q", got, want)
	}
}

// bfc-121s AC-8: options longer than the screen are cut to fit 80x24.
func TestGrillCutsLongOptions(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a very long option that goes on ", 4)
	g := newFakeGrill().withOpenQuestion("Which one?", long+"2", long+"1", long+"2")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "Write my own answer")
	finalGolden(t, tm)
}

// bfc-121s AC-8: the answer line of a Question with options fits 80x24,
// with the options still listed and esc back to them in the help line.
func TestGrillAnswerLineWithOptions(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "EUR", "USD", "EUR")
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› EUR (recommended)")
	pressKey(tm, tea.KeyDown)
	waitFor(t, tm, "› Write my own answer")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "esc options")
	typeText(tm, "CHF")
	waitFor(t, tm, "CHF▌")
	finalGolden(t, tm)
}

// bfc-121s AC-8: while a picked option is being stored, the screen shows it
// on the answer line and fits 80x24.
func TestGrillSavingAPickedOption(t *testing.T) {
	t.Parallel()
	g := newFakeGrill().withOpenQuestion("Which currency?", "EUR", "USD", "EUR")
	g.answerHold = make(chan struct{})
	tm := newGrillModel(t, g)
	waitFor(t, tm, "› EUR (recommended)")
	pressKey(tm, tea.KeyEnter)
	waitFor(t, tm, "EUR  (saving…)")
	finalGolden(t, tm)
	close(g.answerHold)
}
