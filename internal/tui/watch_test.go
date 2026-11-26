package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/admSla99/BeforeCode/internal/core"
)

// watching is the app options of the watch tests: no timer sync, a short
// watch interval, the team as the Watcher.
func watching(f *fakeTeam, local bool) AppOptions {
	return AppOptions{Interval: time.Hour, Local: local, Watcher: f, WatchInterval: 20 * time.Millisecond}
}

// waitReads waits until the Watcher has read the tip n more times.
func waitReads(t *testing.T, f *fakeTeam, n int) {
	t.Helper()
	start, _ := f.counts()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reads, _ := f.counts(); reads >= start+n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the tip was not read %d more times", n)
}

// bfc-z3wd AC-1, AC-5: a plan written by another process appears on the
// open plan list without a key press, with and without sync.
func TestAppShowsAPlanWrittenOutside(t *testing.T) {
	t.Parallel()
	for name, local := range map[string]bool{"synced": false, "local": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFakeTeam()
			app := NewApp(f, f, watching(f, local))
			app.now = func() time.Time { return t0 }
			tm := newTestModel(t, app, 80, 24)
			toPlanList(t, tm)
			f.outside(func() {
				f.plans.plans = append([]PlanRow{{ShortID: "bfc-w4tc", State: core.StateDraft, Title: "Written by the agent", Author: "ada@example.com", LastActivity: at(18, 0)}}, f.plans.plans...)
			})
			waitFor(t, tm, "Written by the agent")
			if out := screen(t, tm); !strings.Contains(out, "bfc-w4tc") {
				t.Errorf("the new plan is not listed:\n%s", out)
			}
		})
	}
}

// bfc-z3wd AC-2: a new version of plan.md written outside appears on the
// open detail; the tab and the scroll position stay.
func TestAppShowsPlanMdWrittenOutsideAndKeepsTheScroll(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	tm := startAppWith(t, f, watching(f, false))
	send(tm, "j", "j", "j", "j", "j", "j")
	waitFor(t, tm, "Wire the feature flag")
	f.outside(func() {
		d := f.details["bfc-5v7x"]
		d.Plan = []byte(strings.Replace(planMD, "Refunds.", "Refunds and chargebacks.", 1))
		f.details["bfc-5v7x"] = d
	})
	waitFor(t, tm, "chargebacks")
	lines := strings.Split(screen(t, tm), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "Add a Stripe client") {
		t.Errorf("scroll position lost; first line is %q", lines[1])
	}
}

// bfc-z3wd AC-2: a Claim and a Handoff written outside appear on the open
// tasks tab.
func TestAppShowsATaskChangedOutside(t *testing.T) {
	t.Parallel()
	f := inProgressTeam()
	tm := startAppWith(t, f, watching(f, true))
	send(tm, "tab", "tab")
	waitFor(t, tm, "T-3")
	f.outside(func() {
		d := f.details["bfc-5v7x"]
		d.Tasks = append(d.Tasks, TaskRow{ID: "T-4", Text: "Write the runbook.", State: core.TaskPaused,
			Holder: "Peter", Latest: "Runbook drafted by the agent."})
		f.details["bfc-5v7x"] = d
	})
	waitFor(t, tm, "Runbook drafted by the agent.")
	if out := screen(t, tm); !strings.Contains(out, "T-1") {
		t.Errorf("the tasks tab is no longer shown:\n%s", out)
	}
}

// bfc-z3wd AC-3: a Message written outside appears on the open Grill, and
// the answer being typed stays.
func TestAppGrillShowsAMessageWrittenOutsideAndKeepsTheAnswer(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	g := newFakeGrill().asking()
	opts := watching(f, true)
	opts.Grill = g
	app := NewApp(f, f, opts)
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	toPlanList(t, tm)
	send(tm, "enter")
	waitFor(t, tm, "g grill")
	send(tm, "g")
	waitFor(t, tm, "Enter starts the Grill")
	send(tm, "enter")
	waitFor(t, tm, "send answer")
	typeText(tm, "Half typed")
	waitFor(t, tm, "Half typed")
	g.mu.Lock()
	g.snap.Others = append(g.snap.Others, GrillMessage{Who: "Peter", Text: "Comment from the CLI."})
	g.mu.Unlock()
	f.outside(func() {})
	waitFor(t, tm, "Comment from the CLI.")
	if out := screen(t, tm); !strings.Contains(out, "Half typed") {
		t.Errorf("the answer being typed is lost:\n%s", out)
	}
}

// bfc-z3wd AC-4: a Question written outside appears on the open Inbox.
func TestInboxShowsAQuestionWrittenOutside(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	f.plans.waiting = waitingOnAda()
	app := NewApp(f, f, watching(f, true))
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "Questions (1)")
	f.outside(func() {
		q := f.plans.waiting.Questions[0]
		q.Message.Body = "Asked by the agent."
		f.plans.waiting.Questions = append(f.plans.waiting.Questions, q)
	})
	waitFor(t, tm, "Questions (2)")
	if out := screen(t, tm); !strings.Contains(out, "Asked by the agent.") {
		t.Errorf("the new Question is not listed:\n%s", out)
	}
}

// bfc-z3wd AC-6: while the tip stays, the watcher only reads it.
func TestAppDoesNotReloadWhileTheTipStays(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	tm := startAppWith(t, f, watching(f, true))
	waitReads(t, f, 1) // the loads of the detail have arrived
	_, before := f.counts()
	waitReads(t, f, 4)
	_, after := f.counts()
	if out := screen(t, tm); !strings.Contains(out, "Affected areas") {
		t.Errorf("the detail is no longer shown:\n%s", out)
	}
	if after != before {
		t.Errorf("%d loads while the tip stayed, want none", after-before)
	}
}

// bfc-z3wd AC-8: a sync that brought changes reloads the screens once:
// the watcher does not reload again for the tip the sync made.
func TestAppSyncAndWatcherReloadOnce(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	release := make(chan struct{})
	f.syncFn = func() SyncResult {
		<-release
		f.outside(func() {
			f.plans.plans = append([]PlanRow{{ShortID: "bfc-w4tc", State: core.StateDraft, Title: "Pulled by the sync", Author: "ada@example.com", LastActivity: at(18, 0)}}, f.plans.plans...)
		})
		return SyncResult{Outcome: SyncOK, Changed: true}
	}
	app := NewApp(f, f, watching(f, false))
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	toPlanList(t, tm)
	waitReads(t, f, 1)
	_, before := f.counts()
	close(release)
	waitFor(t, tm, "Pulled by the sync")
	waitReads(t, f, 4)
	_, after := f.counts()
	screen(t, tm)
	// One reload loads the Inbox and the list.
	if after-before != 2 {
		t.Errorf("%d loads after the sync, want 2 (one reload)", after-before)
	}
}

// bfc-z3wd: a read asked for while one runs waits for it, so two reads
// never run at once and an older tip never overwrites a newer one.
func TestAppReadsOneTipAtATime(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	m := NewApp(f, f, watching(f, true))
	first := m.readTip()
	if first == nil || m.readTip() != nil {
		t.Fatal("a second read started while the first runs")
	}
	f.outside(func() {})
	next, _ := m.Update(first())
	m = next.(App)
	if !m.watch.reading || m.watch.again {
		t.Fatal("the queued read did not start after the first ended")
	}
}

// bfc-z3wd AC-7: a plan that appears above the selected one keeps the same
// plan selected.
func TestAppKeepsTheSelectedPlanWhenOneAppearsAbove(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	app := NewApp(f, f, watching(f, true))
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	toPlanList(t, tm)
	send(tm, "j", "j")
	waitFor(t, tm, "▶ Rate-limit the public API")
	f.outside(func() {
		f.plans.plans = append([]PlanRow{{ShortID: "bfc-w4tc", State: core.StateInReview, Title: "Written by the agent", Author: "ada@example.com", LastActivity: at(18, 0)}}, f.plans.plans...)
	})
	waitFor(t, tm, "Written by the agent")
	if out := screen(t, tm); !strings.Contains(out, "▶ Rate-limit the public API") {
		t.Errorf("the selection moved to another plan:\n%s", out)
	}
}

// bfc-z3wd AC-7: an Inbox item that appears above the selected one keeps
// the same item selected.
func TestInboxKeepsTheSelectedItemWhenOneAppearsAbove(t *testing.T) {
	t.Parallel()
	f := newFakeTeam()
	f.plans.waiting = waitingOnAda()
	app := NewApp(f, f, watching(f, true))
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "Questions (1)")
	send(tm, "j", "j")
	waitFor(t, tm, "▶ bfc-k2m9")
	f.outside(func() {
		f.plans.waiting.Reviews = append([]core.PlanRef{{ShortID: "bfc-w4tc", Title: "Written by the agent"}}, f.plans.waiting.Reviews...)
	})
	waitFor(t, tm, "Reviews (2)")
	if out := screen(t, tm); !strings.Contains(out, "▶ bfc-k2m9") {
		t.Errorf("the selection moved to another item:\n%s", out)
	}
}
