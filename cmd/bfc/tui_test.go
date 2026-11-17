package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/tui"
)

// adapterIn returns the TUI's Plans adapter for the repository in dir.
func adapterIn(t *testing.T, dir string) tui.Plans {
	t.Helper()
	t.Chdir(dir)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	return &tuiPlans{cmd: cmd}
}

func TestTUIAdapterLoadsThePlansLikeBfcList(t *testing.T) {
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
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	_, err := adapterIn(t, dir).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || nse.Advice != "" {
		t.Errorf("err = %#v, want NotSetUpError with the general advice", err)
	}
}

func TestTUIAdapterGivesThePreciseAdviceWhenTheRemoteHasPlans(t *testing.T) {
	_, bare := sharedProject(t, "First")
	ben := plainClone(t, bare)
	_, err := adapterIn(t, ben).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || !strings.Contains(nse.Advice, "bfc bootstrap") {
		t.Errorf("err = %#v, want advice to run bfc bootstrap", err)
	}
}

func TestBareBfcPrintsUsageWhenNotInATerminal(t *testing.T) {
	out := run(t, "dev")
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "tui") {
		t.Errorf("bfc without a terminal:\n%s", out)
	}
}

func TestTUIRefusesWithoutATerminal(t *testing.T) {
	dir := newLocalRepo(t)
	if out, err := bfcIn(t, dir, "tui"); err == nil || !strings.Contains(out, "terminal") {
		t.Errorf("bfc tui without a terminal: %v\n%s", err, out)
	}
}

func TestTUIAdapterGivesNoAdviceWhenTheRemoteHasNoPlans(t *testing.T) {
	ana, _ := newClone(t)
	_, err := adapterIn(t, ana).Load(context.Background())
	var nse *tui.NotSetUpError
	if !errors.As(err, &nse) || nse.Advice != "" {
		t.Errorf("err = %#v, want NotSetUpError without advice (the screen already lists bfc init)", err)
	}
}
