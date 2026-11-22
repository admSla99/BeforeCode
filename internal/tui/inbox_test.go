package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// waitingOnAda is what waits on Ada in the team of team(): two Findings on
// her plan, a review and a Question.
func waitingOnAda() core.Waiting {
	mine := core.PlanRef{ShortID: "bfc-5v7x", Title: "Payments via Stripe"}
	return core.Waiting{
		Findings: []core.WaitingFinding{
			{PlanRef: mine, Finding: core.Finding{PR: 42, Criterion: "AC-2"}},
			{PlanRef: mine, Finding: core.Finding{PR: 42, OutOfScope: "Changes the retry policy of the email worker."}},
		},
		Reviews: []core.PlanRef{{ShortID: "bfc-k2m9", Title: "Rate-limit the public API"}},
		Questions: []core.WaitingQuestion{{PlanRef: mine, Message: core.Message{
			ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E"), Author: core.Person{Email: "ben@example.com"},
			To: []string{"ada@example.com"}, Body: "Which cards fail?\nAll of them?",
		}}},
	}
}

// runInbox starts the Inbox screen alone at w x h, waits for want, sends
// the keys and returns the final screen without styling.
func runInbox(t *testing.T, plans Plans, w, h int, want string, keys ...string) string {
	t.Helper()
	tm := newTestModel(t, NewInbox(plans), w, h)
	waitFor(t, tm, want)
	send(tm, keys...)
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	return ansi.Strip(tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).View().Content)
}

// Issue #106 AC-3, AC-4: three sections with a rule and a count; a Finding
// shows the pull request, the AC or out-of-scope text and the Short ID.
func TestInboxListsWhatWaits(t *testing.T) {
	f := &fakePlans{waiting: waitingOnAda()}
	golden(t, runInbox(t, f, 80, 24, "Questions (1)", "down", "down"))
}

// Issue #106 AC-3: one line when nothing waits.
func TestInboxNothingWaits(t *testing.T) {
	golden(t, runInbox(t, &fakePlans{waiting: core.Waiting{}}, 80, 24, "Nothing waits on you"))
}

func TestInboxNotSetUp(t *testing.T) {
	out := runInbox(t, &fakePlans{err: &NotSetUpError{}}, 80, 24, "not set up")
	if !strings.Contains(out, "bfc init") {
		t.Errorf("no advice:\n%s", out)
	}
}

// Issue #106 AC-3, AC-5: many items scroll and every line fits 80x24.
func TestInboxFits80x24(t *testing.T) {
	w := waitingOnAda()
	for range 30 {
		w.Reviews = append(w.Reviews, core.PlanRef{ShortID: "bfc-zzzz", Title: strings.Repeat("A very long title ", 8)})
	}
	keys := make([]string, 0, 40)
	for range 40 {
		keys = append(keys, "j")
	}
	out := runInbox(t, &fakePlans{waiting: w}, 80, 24, "Reviews (31)", keys...)
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Errorf("%d lines, want 24", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 80 {
			t.Errorf("line wider than 80: %q", l)
		}
	}
	if !strings.Contains(lines[len(lines)-1], "tab plans") || !strings.Contains(out, "▶ bfc-5v7x  from ben@example.com: Which cards fail?") {
		t.Errorf("the selection or the help line is not shown:\n%s", out)
	}
}

// Issue #106 AC-3: the app starts on the Inbox; enter opens the plan of the
// selected item, esc returns to the Inbox, tab switches to the plan list
// and back.
func TestAppStartsOnTheInbox(t *testing.T) {
	f := newFakeTeam()
	f.plans.waiting = waitingOnAda()
	app := NewApp(f, f, AppOptions{Interval: time.Hour, Local: true})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "Findings (2)")
	send(tm, "enter")
	waitFor(t, tm, "Affected areas")
	send(tm, "esc")
	waitFor(t, tm, "Findings (2)")
	send(tm, "tab")
	waitFor(t, tm, "IN-REVIEW")
	send(tm, "tab")
	waitFor(t, tm, "Findings (2)")
	out := screen(t, tm)
	if !strings.Contains(out, "BeforeCode · inbox") {
		t.Errorf("not back on the Inbox:\n%s", out)
	}
}

// Issue #106 AC-4: after a sync that brings an Acknowledgement or a clean
// check, the Finding is gone.
func TestInboxDropsAFindingAfterASync(t *testing.T) {
	f := newFakeTeam()
	f.plans.waiting = waitingOnAda()
	synced := make(chan struct{})
	f.syncFn = func() SyncResult {
		<-synced
		f.mu.Lock()
		f.plans.waiting.Findings = nil
		f.plans.waiting.Reviews = append(f.plans.waiting.Reviews, core.PlanRef{ShortID: "bfc-t5y2", Title: "Retry pushes with backoff"})
		f.mu.Unlock()
		return SyncResult{Outcome: SyncOK, Changed: true}
	}
	app := NewApp(f, f, AppOptions{Interval: time.Hour})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "Findings (2)")
	close(synced)
	// Only the Inbox reloaded after the sync shows the second review.
	waitFor(t, tm, "Reviews (2)")
	out := screen(t, tm)
	if strings.Contains(out, "Findings") || strings.Contains(out, "PR #42") {
		t.Errorf("the Finding is still shown after the sync:\n%s", out)
	}
}
