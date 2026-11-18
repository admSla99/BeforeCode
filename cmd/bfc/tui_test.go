package main

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/tui"
)

// adapterIn returns the TUI's Plans adapter for the repository in dir.
func adapterIn(t *testing.T, dir string) tui.Plans {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(inPlace(t, dir))
	return &tuiPlans{cmd: cmd}
}

func TestTUIAdapterLoadsThePlansLikeBfcList(t *testing.T) {
	t.Parallel()
	clock(t, 10)
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	clock(t, 30)
	other, _ := newPlan(t, dir, "Newer plan")

	plans, err := adapterIn(t, dir).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %+v", plans)
	}
	got := plans[0]
	if got.ShortID != other || got.State != core.StateDraft || got.Title != "Newer plan" || got.Author != adaP.Email || got.LastActivity.IsZero() {
		t.Errorf("newest plan = %+v", got)
	}
	if plans[1].ShortID != short || plans[1].State != core.StateInReview || plans[1].Title != "Payments" {
		t.Errorf("older plan = %+v", plans[1])
	}
}

func TestTUIAdapterReportsAMissingPlansRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	_, err := adapterIn(t, dir).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || nse.Advice != "" {
		t.Errorf("err = %#v, want NotSetUpError with the general advice", err)
	}
}

func TestTUIAdapterGivesThePreciseAdviceWhenTheRemoteHasPlans(t *testing.T) {
	t.Parallel()
	_, bare := sharedProject(t, "First")
	ben := plainClone(t, bare)
	_, err := adapterIn(t, ben).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || !strings.Contains(nse.Advice, "bfc bootstrap") {
		t.Errorf("err = %#v, want advice to run bfc bootstrap", err)
	}
}

func TestBareBfcPrintsUsageWhenNotInATerminal(t *testing.T) {
	t.Parallel()
	out := run(t, "dev")
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "tui") {
		t.Errorf("bfc without a terminal:\n%s", out)
	}
}

func TestTUIRefusesWithoutATerminal(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	if out, err := bfcIn(t, dir, "tui"); err == nil || !strings.Contains(out, "terminal") {
		t.Errorf("bfc tui without a terminal: %v\n%s", err, out)
	}
}

func TestTUIAdapterGivesNoAdviceWhenTheRemoteHasNoPlans(t *testing.T) {
	t.Parallel()
	ana, _ := newClone(t)
	_, err := adapterIn(t, ana).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || nse.Advice != "" {
		t.Errorf("err = %#v, want NotSetUpError without advice (the screen already lists bfc init)", err)
	}
}

func reviewAdapter(t *testing.T, dir string) *tuiPlans {
	t.Helper()
	return adapterIn(t, dir).(*tuiPlans)
}

// The detail carries the actions valid for the current user.
func TestTUIAdapterDetailListsTheActionsOfTheCurrentUser(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	ctx := context.Background()

	d, err := reviewAdapter(t, dir).Detail(ctx, short)
	if err != nil {
		t.Fatal(err)
	}
	if d.ShortID != short || d.Title != "Payments" || d.State != core.StateDraft || d.Author != adaP.Email ||
		!strings.Contains(string(d.Plan), "## Acceptance criteria") || !strings.Contains(string(d.Intent), "title: Payments") {
		t.Errorf("detail = %+v", d)
	}
	if !slices.Equal(d.Actions, []tui.ReviewAction{tui.ActionSubmit, tui.ActionClose}) || !d.Editable {
		t.Errorf("author's actions on a draft = %v (editable %v), want submit and close", d.Actions, d.Editable)
	}

	mustBfc(t, dir, "submit", short)
	if d, _ := reviewAdapter(t, dir).Detail(ctx, short); !slices.Equal(d.Actions, []tui.ReviewAction{tui.ActionClose}) || d.RequiredApprovals != 1 {
		t.Errorf("author's actions in review = %v, required %d", d.Actions, d.RequiredApprovals)
	}
	as(t, dir, janaP)
	d, _ = reviewAdapter(t, dir).Detail(ctx, short)
	if !slices.Equal(d.Actions, []tui.ReviewAction{tui.ActionApprove, tui.ActionRequestChanges, tui.ActionClose}) {
		t.Errorf("reviewer's actions = %v", d.Actions)
	}
}

func TestTUIAdapterReviewWritesLikeTheCLI(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	ctx := context.Background()
	if err := reviewAdapter(t, dir).Review(ctx, short, tui.ActionSubmit, ""); err != nil {
		t.Fatal(err)
	}
	if err := reviewAdapter(t, dir).Review(ctx, short, tui.ActionApprove, ""); err == nil || !strings.Contains(err.Error(), "your own plan") {
		t.Errorf("approve by the author: %v", err)
	}
	as(t, dir, janaP)
	if err := reviewAdapter(t, dir).Review(ctx, short, tui.ActionRequestChanges, "Split AC-1."); err != nil {
		t.Fatal(err)
	}
	e := last(t, dir, id)
	if e.Type != core.ChangesRequested || e.Message == "" {
		t.Errorf("event = %+v", e)
	}
	if got := state(t, dir, short); got != "draft" {
		t.Errorf("state = %s", got)
	}
}

func TestTUISyncerReportsEachOutcome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	t.Run("ok and changed", func(t *testing.T) {
		t.Parallel()
		ana, ben, _ := twoClones(t)
		if r := syncerIn(t, ben).Sync(ctx); r.Outcome != tui.SyncOK || r.Changed {
			t.Errorf("nothing new: %+v", r)
		}
		mustBfc(t, ana, "new", "--no-edit", "From Ana")
		mustBfc(t, ana, "push")
		if r := syncerIn(t, ben).Sync(ctx); r.Outcome != tui.SyncOK || !r.Changed {
			t.Errorf("ana's plan: %+v", r)
		}
	})
	t.Run("offline", func(t *testing.T) {
		t.Parallel()
		ana, _ := newClone(t)
		mustBfc(t, ana, "init")
		gitIn(t, ana, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
		r := syncerIn(t, ana).Sync(ctx)
		if r.Outcome != tui.SyncOffline || !strings.Contains(r.Message, "does not appear to be a git repository") {
			t.Errorf("result = %+v", r)
		}
	})
	t.Run("conflict", func(t *testing.T) {
		t.Parallel()
		ana, ben, _ := twoClones(t)
		mustBfc(t, ana, "new", "--no-edit", "Shared plan")
		mustBfc(t, ana, "push")
		mustBfc(t, ben, "pull")
		editIntent(t, ana, `Ana wants this.\n`)
		editIntent(t, ben, `Ben wants that.\n`)
		mustBfc(t, ana, "push")
		r := syncerIn(t, ben).Sync(ctx)
		if r.Outcome != tui.SyncConflict || r.Message != "intent.md" {
			t.Errorf("result = %+v", r)
		}
	})
	t.Run("local mode", func(t *testing.T) {
		t.Parallel()
		if r := syncerIn(t, newLocalRepo(t)).Sync(ctx); r.Outcome != tui.SyncLocalOnly {
			t.Errorf("result = %+v", r)
		}
	})
}

func syncerIn(t *testing.T, dir string) tui.Syncer {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(inPlace(t, dir))
	return &tuiSyncer{cmd: cmd}
}

// Submit is offered only when core.CheckSubmit allows it, and a closed plan
// is not editable.
func TestTUIAdapterHidesSubmitForAnUnreadyPlanAndEditForAClosedOne(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	ctx := context.Background()
	if d, _ := reviewAdapter(t, dir).Detail(ctx, short); !slices.Equal(d.Actions, []tui.ReviewAction{tui.ActionClose}) || !d.Editable {
		t.Errorf("draft without plan.md: actions %v, editable %v", d.Actions, d.Editable)
	}
	addEvent(t, dir, id, core.Event{Type: core.Closed, Author: adaP})
	if d, _ := reviewAdapter(t, dir).Detail(ctx, short); d.Editable || len(d.Actions) != 0 {
		t.Errorf("closed plan: actions %v, editable %v", d.Actions, d.Editable)
	}
}

// #49: close through the TUI writes what bfc close writes; the reason is
// optional.
func TestTUIAdapterClosesLikeTheCLI(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	as(t, dir, janaP)
	if err := reviewAdapter(t, dir).Review(context.Background(), short, tui.ActionClose, ""); err != nil {
		t.Fatal(err)
	}
	if e := last(t, dir, id); e.Type != core.Closed || e.Author.Email != janaP.Email || e.Message != "" {
		t.Errorf("event = %+v", e)
	}
	if got := state(t, dir, short); got != "closed" {
		t.Errorf("state = %s", got)
	}
}

// An open Question hides approve in the TUI, like bfc approve refuses it.
func TestTUIAdapterHidesApproveWhileAQuestionIsOpen(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Why?")
	d, err := reviewAdapter(t, dir).Detail(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Actions, []tui.ReviewAction{tui.ActionRequestChanges, tui.ActionClose}) {
		t.Errorf("reviewer's actions with an open Question = %v", d.Actions)
	}
}
