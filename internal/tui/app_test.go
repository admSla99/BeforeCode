package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

const planMD = `## Context

The checkout calls payments.Charge, which wraps the old provider.

## Approach

Add a Stripe client behind the payments.Provider interface and switch EU
traffic with a feature flag.

## Affected areas

- payments/
- checkout/handler.go

## Out of scope

Refunds.

## Tasks

- Add the Stripe client.
- Wire the feature flag.
- Remove the old provider once EU traffic is moved.

## Acceptance criteria

- AC-1: With an invalid card the API returns 402.
- AC-2: A successful payment sends exactly one confirmation email.
- AC-3: The feature flag can switch back without a deploy.
`

const intentMD = `---
title: Payments via Stripe
---

## Problem

Cards fail at checkout for 3% of EU customers.
`

// fakeTeam is the test adapter of Plans and Syncer for the whole app.
type fakeTeam struct {
	mu       sync.Mutex
	plans    *fakePlans
	details  map[string]PlanDetail
	reviews  []string // "action shortID reason"
	edits    []string // "shortID doc"
	reviewFn func(a ReviewAction) error
	noChange bool // the editor closes without a change
	// editFn, when set, is what the editor does to the plan before it
	// closes; it runs with mu held.
	editFn func(shortID string, doc Doc)
	syncs  int
	syncFn func() SyncResult
}

func newFakeTeam() *fakeTeam {
	return &fakeTeam{plans: team(), details: map[string]PlanDetail{
		"bfc-5v7x": {
			ShortID: "bfc-5v7x", Title: "Payments via Stripe", State: core.StateInReview,
			Author: "jana@example.com", RequiredApprovals: 2, Approvers: []string{"peter@example.com"},
			Intent: []byte(intentMD), Plan: []byte(planMD),
			Actions: []ReviewAction{ActionApprove, ActionRequestChanges}, Editable: true,
		},
	}}
}

func (f *fakeTeam) Load(ctx context.Context) ([]PlanRow, error) { return f.plans.Load(ctx) }
func (f *fakeTeam) NewPlan(title string) tea.Cmd                { return f.plans.NewPlan(title) }

func (f *fakeTeam) Detail(_ context.Context, shortID string) (PlanDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.details[shortID]
	if !ok {
		return PlanDetail{}, errors.New("no plan " + shortID)
	}
	return d, nil
}

func (f *fakeTeam) Edit(shortID string, doc Doc) tea.Cmd {
	return func() tea.Msg {
		f.mu.Lock()
		f.edits = append(f.edits, shortID+" "+doc.String())
		if f.editFn != nil {
			f.editFn(shortID, doc)
		}
		changed := !f.noChange
		f.mu.Unlock()
		return PlanEditedMsg{Changed: changed}
	}
}

func (f *fakeTeam) Review(_ context.Context, shortID string, a ReviewAction, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reviews = append(f.reviews, strings.TrimSpace(string(a)+" "+shortID+" "+reason))
	if f.reviewFn != nil {
		return f.reviewFn(a)
	}
	return nil
}

func (f *fakeTeam) Sync(context.Context) SyncResult {
	f.mu.Lock()
	f.syncs++
	fn := f.syncFn
	f.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return SyncResult{Outcome: SyncOK}
}

func (f *fakeTeam) count(what *[]string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), *what...)
}

// startApp runs the app at 80x24 with a long sync interval and a fixed
// clock, and waits until the list shows.
func startApp(t *testing.T, f *fakeTeam) *teatest.TestModel {
	t.Helper()
	app := NewApp(f, f, AppOptions{Interval: time.Hour})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "IN-REVIEW")
	return tm
}

func send(tm *teatest.TestModel, keys ...string) {
	for _, k := range keys {
		tm.Send(press(k))
	}
}

// screen quits and returns the final screen without styling.
func screen(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	return ansi.Strip(tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).View().Content)
}

// AC-1: enter opens the detail with the header and plan.md rendered.
func TestAppEnterOpensThePlanDetail(t *testing.T) {
	// Local mode keeps the timer-dependent sync status out of the golden file.
	tm := startAppWith(t, newFakeTeam(), AppOptions{Interval: time.Hour, Local: true})
	golden(t, screen(t, tm))
}

// AC-1: tab switches between plan.md and intent.md.
func TestAppTabShowsTheIntent(t *testing.T) {
	tm := startAppWith(t, newFakeTeam(), AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab")
	waitFor(t, tm, "Problem")
	golden(t, screen(t, tm))
}

// AC-7: esc returns to the list.
func TestAppEscReturnsToTheList(t *testing.T) {
	tm := startApp(t, newFakeTeam())
	send(tm, "enter")
	waitFor(t, tm, "Affected areas")
	send(tm, "esc")
	waitFor(t, tm, "IN-REVIEW")
	out := screen(t, tm)
	if !strings.Contains(out, "BeforeCode · plans") || strings.Contains(out, "Affected areas") {
		t.Errorf("not back on the list:\n%s", out)
	}
}

func TestAppDetailOfAPlanWithoutPlanMd(t *testing.T) {
	f := newFakeTeam()
	d := f.details["bfc-5v7x"]
	d.Plan, d.State, d.Actions = nil, core.StateDraft, nil
	f.details["bfc-5v7x"] = d
	tm := startApp(t, f)
	send(tm, "enter")
	waitFor(t, tm, "Problem") // a plan without plan.md opens on intent.md
	send(tm, "tab")
	waitFor(t, tm, "No plan.md yet")
	out := screen(t, tm)
	if !strings.Contains(out, "Press e to write it") {
		t.Errorf("missing plan.md not explained:\n%s", out)
	}
}

// openDetail starts the app and opens the plan bfc-5v7x.
func openDetail(t *testing.T, f *fakeTeam) *teatest.TestModel {
	t.Helper()
	tm := startApp(t, f)
	send(tm, "enter")
	waitFor(t, tm, "Affected areas")
	return tm
}

// AC-3: an action the plan does not allow does nothing; one it allows
// calls Review, shows the result and syncs.
func TestAppApproveCallsReviewAndSyncs(t *testing.T) {
	f := newFakeTeam()
	tm := openDetail(t, f)
	before := f.syncCount()
	send(tm, "s", "a")
	waitFor(t, tm, "approve: done")
	out := screen(t, tm)
	if got := f.count(&f.reviews); len(got) != 1 || got[0] != "approve bfc-5v7x" {
		t.Errorf("reviews = %q, want only the approval", got)
	}
	if f.syncCount() <= before {
		t.Error("no sync after the approval")
	}
	if strings.Contains(out, "s submit") {
		t.Errorf("help offers submit, which this plan does not allow:\n%s", out)
	}
}

// AC-3: a refusal is a message on the screen, not a crash.
func TestAppShowsARefusal(t *testing.T) {
	f := newFakeTeam()
	f.reviewFn = func(ReviewAction) error {
		return errors.New("bfc-5v7x is your own plan; someone other than the plan author must approve it")
	}
	tm := openDetail(t, f)
	before := f.syncCount()
	send(tm, "a")
	waitFor(t, tm, "your own plan")
	out := screen(t, tm)
	if !strings.Contains(out, "is your own plan") || !strings.Contains(out, "Affected areas") {
		t.Errorf("refusal not shown on the detail:\n%s", out)
	}
	if f.syncCount() != before {
		t.Error("a refused action started a sync")
	}
}

// AC-3: r asks for the reason, then requests changes with it.
func TestAppRequestChangesAsksForTheReason(t *testing.T) {
	f := newFakeTeam()
	// Local mode keeps the timer-dependent sync status out of the golden file.
	tm := startAppWith(t, f, AppOptions{Interval: time.Hour, Local: true})
	send(tm, "r", "x", "esc", "r", " ", "enter", "r")
	send(tm, strings.Split("Split AC-1", "")...)
	// The first r already showed the prompt; wait for the typed reason.
	waitFor(t, tm, "Reason: Split AC-1")
	golden(t, screenAfter(t, tm, "enter", "request-changes: done"))
	if got := f.count(&f.reviews); len(got) != 1 || got[0] != "request-changes bfc-5v7x Split AC-1" {
		t.Errorf("reviews = %q", got)
	}
}

// AC-2: e opens the shown document in the editor, then the screen reloads.
func TestAppEditOpensTheShownDocument(t *testing.T) {
	f := newFakeTeam()
	f.editFn = func(shortID string, doc Doc) {
		if doc == DocIntent {
			d := f.details[shortID]
			d.Intent = []byte(intentMD + "\nAlso Apple Pay.\n")
			f.details[shortID] = d
		}
	}
	tm := openDetail(t, f)
	// Each edit command runs in its own goroutine. The real editor blocks
	// the terminal until it exits, so wait for one edit before the next.
	send(tm, "e")
	waitEdits(t, f, 1)
	send(tm, "tab", "e")
	waitFor(t, tm, "Apple Pay")
	screen(t, tm)
	if got := f.count(&f.edits); len(got) != 2 || got[0] != "bfc-5v7x plan.md" || got[1] != "bfc-5v7x intent.md" {
		t.Errorf("edits = %q", got)
	}
}

// waitEdits waits until the fake has seen n edit commands.
func waitEdits(t *testing.T, f *fakeTeam, n int) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); len(f.count(&f.edits)) < n; {
		if time.Now().After(deadline) {
			t.Fatalf("saw %d edits, want %d", len(f.count(&f.edits)), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// screenAfter sends a key, waits for text and returns the final screen.
func screenAfter(t *testing.T, tm *teatest.TestModel, k, want string) string {
	t.Helper()
	send(tm, k)
	waitFor(t, tm, want)
	return screen(t, tm)
}

func (f *fakeTeam) syncCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.syncs
}

func startAppWith(t *testing.T, f *fakeTeam, opts AppOptions) *teatest.TestModel {
	t.Helper()
	app := NewApp(f, f, opts)
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "IN-REVIEW")
	send(tm, "enter")
	waitFor(t, tm, "Affected areas")
	return tm
}

// AC-4: the status bar shows offline and conflict.
func TestAppShowsOfflineAndConflict(t *testing.T) {
	for name, r := range map[string]SyncResult{
		"offline":  {Outcome: SyncOffline, Message: "Could not resolve host: github.com"},
		"conflict": {Outcome: SyncConflict, Message: "plan.md"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeTeam()
			tm := startAppWith(t, f, AppOptions{Interval: time.Hour})
			f.mu.Lock()
			f.syncFn = func() SyncResult { return r }
			f.mu.Unlock()
			send(tm, "a") // a local change syncs at once
			waitFor(t, tm, map[string]string{"offline": "waiting", "conflict": "--keep remote"}[name])
			golden(t, screen(t, tm))
		})
	}
}

// AC-4: the timer syncs on the interval.
func TestAppSyncsOnTheInterval(t *testing.T) {
	f := newFakeTeam()
	tm := startAppWith(t, f, AppOptions{Interval: 20 * time.Millisecond})
	deadline := time.Now().Add(3 * time.Second)
	for f.syncCount() < 4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	screen(t, tm)
	if n := f.syncCount(); n < 4 {
		t.Errorf("%d syncs, want at least 4", n)
	}
}

// AC-5: a sync that changes the open plan refreshes it and keeps the
// scroll position.
func TestAppSyncUpdatesTheOpenPlanAndKeepsTheScroll(t *testing.T) {
	f := newFakeTeam()
	changed := make(chan struct{})
	var once sync.Once
	f.syncFn = func() SyncResult {
		select {
		case <-changed:
			r := SyncResult{Outcome: SyncOK}
			once.Do(func() { r.Changed = true })
			return r
		default:
			return SyncResult{Outcome: SyncOK}
		}
	}
	tm := startAppWith(t, f, AppOptions{Interval: 20 * time.Millisecond})
	send(tm, "j", "j", "j", "j", "j", "j")
	waitFor(t, tm, "Wire the feature flag")
	f.mu.Lock()
	d := f.details["bfc-5v7x"]
	d.Plan = []byte(strings.Replace(planMD, "Refunds.", "Refunds and chargebacks.", 1))
	f.details["bfc-5v7x"] = d
	f.mu.Unlock()
	close(changed)
	waitFor(t, tm, "chargebacks")
	lines := strings.Split(screen(t, tm), "\n")
	// The status bar changes with the timer, so check the document only.
	if !strings.HasPrefix(strings.TrimSpace(lines[1]), "Add a Stripe client") {
		t.Errorf("scroll position lost; first line is %q", lines[1])
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Refunds and chargebacks.") {
		t.Error("the open plan was not refreshed")
	}
}

// AC-6: a slow sync does not block the screen.
func TestAppStaysUsableDuringASlowSync(t *testing.T) {
	f := newFakeTeam()
	release := make(chan struct{})
	f.syncFn = func() SyncResult { <-release; return SyncResult{Outcome: SyncOK} }
	app := NewApp(f, f, AppOptions{Interval: time.Hour})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	defer close(release)
	waitFor(t, tm, "IN-REVIEW")
	send(tm, "enter")
	// The list already shows the sync status, so wait for the detail first.
	waitFor(t, tm, "Affected areas")
	waitFor(t, tm, "⟳ syncing")
	send(tm, "tab")
	waitFor(t, tm, "Problem")
	screen(t, tm)
}

// AC-4: local mode never syncs and says so.
func TestAppLocalModeNeverSyncs(t *testing.T) {
	f := newFakeTeam()
	tm := startAppWith(t, f, AppOptions{Interval: 10 * time.Millisecond, Local: true})
	send(tm, "a")
	waitFor(t, tm, "approve: done")
	time.Sleep(50 * time.Millisecond)
	out := screen(t, tm)
	if f.syncCount() != 0 || !strings.Contains(out, "local mode, not synced") {
		t.Errorf("syncs = %d\n%s", f.syncCount(), out)
	}
}

// An editor closed without a change writes nothing, so nothing syncs.
func TestAppUnchangedEditDoesNotSync(t *testing.T) {
	f := newFakeTeam()
	f.noChange = true
	tm := openDetail(t, f)
	before := f.syncCount()
	send(tm, "e")
	time.Sleep(100 * time.Millisecond)
	screen(t, tm)
	if len(f.count(&f.edits)) != 1 || f.syncCount() != before {
		t.Errorf("edits %q, syncs %d → %d", f.count(&f.edits), before, f.syncCount())
	}
}

// AC-3: a closed plan cannot be edited, so e is neither shown nor taken.
func TestAppHidesEditOnAClosedPlan(t *testing.T) {
	f := newFakeTeam()
	d := f.details["bfc-5v7x"]
	d.State, d.Actions, d.Editable = core.StateClosed, nil, false
	f.details["bfc-5v7x"] = d
	tm := openDetail(t, f)
	send(tm, "e")
	time.Sleep(100 * time.Millisecond)
	out := screen(t, tm)
	if strings.Contains(out, "e edit") || len(f.count(&f.edits)) != 0 {
		t.Errorf("edit offered on a closed plan:\n%s", out)
	}
	if !strings.Contains(out, "scroll") {
		t.Errorf("help line lacks the scroll keys:\n%s", out)
	}
}

// AC-4: the list shows the sync status too.
func TestAppListShowsTheSyncStatus(t *testing.T) {
	f := newFakeTeam()
	f.syncFn = func() SyncResult {
		return SyncResult{Outcome: SyncOffline, Message: "Could not resolve host: github.com"}
	}
	app := NewApp(f, f, AppOptions{Interval: time.Hour})
	app.now = func() time.Time { return t0 }
	tm := newTestModel(t, app, 80, 24)
	waitFor(t, tm, "Could not resolve")
	golden(t, screen(t, tm))
}

// openDetailWith opens the detail of bfc-5v7x with these actions, in local
// mode, which keeps the timer-dependent sync status out of golden files.
func openDetailWith(t *testing.T, f *fakeTeam, actions ...ReviewAction) *teatest.TestModel {
	t.Helper()
	d := f.details["bfc-5v7x"]
	d.Actions = actions
	f.details["bfc-5v7x"] = d
	return startAppWith(t, f, AppOptions{Interval: time.Hour, Local: true})
}

// #49 AC-5: c asks for confirmation on one line with an optional reason;
// esc cancels, enter closes.
func TestAppCloseAsksToConfirmWithAnOptionalReason(t *testing.T) {
	f := newFakeTeam()
	tm := openDetailWith(t, f, ActionApprove, ActionClose)
	send(tm, "c")
	waitFor(t, tm, "Close bfc-5v7x? Reason (optional):")
	send(tm, "esc", "c")
	send(tm, strings.Split("Done elsewhere", "")...)
	golden(t, screenAfter(t, tm, "enter", "close: done"))
	if got := f.count(&f.reviews); len(got) != 1 || got[0] != "close bfc-5v7x Done elsewhere" {
		t.Errorf("reviews = %q", got)
	}
}

func TestAppCloseWithoutAReason(t *testing.T) {
	f := newFakeTeam()
	tm := openDetailWith(t, f, ActionClose)
	send(tm, "c", "enter")
	waitFor(t, tm, "close: done")
	screen(t, tm)
	if got := f.count(&f.reviews); len(got) != 1 || got[0] != "close bfc-5v7x" {
		t.Errorf("reviews = %q", got)
	}
}

// #49 AC-5: without the action, c does nothing and is not in the help line.
func TestAppCloseIsOfferedOnlyWhenAllowed(t *testing.T) {
	f := newFakeTeam()
	tm := openDetailWith(t, f, ActionApprove)
	send(tm, "c")
	out := screen(t, tm)
	if strings.Contains(out, "Close bfc-5v7x?") || strings.Contains(out, "c close") {
		t.Errorf("close offered without the action:\n%s", out)
	}
}
