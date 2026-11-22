package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/tui"
)

// Issue #111 AC-1, AC-2, AC-3: bfc done writes the done Event, with the note
// as a Message, after warning about Tasks that are not completed and
// Findings that wait; it does not refuse for them.
func TestDoneFinishesAPlan(t *testing.T) {
	t.Parallel()
	dir, short, id := claimedPlan(t)
	recordResult(t, dir, short, "7", sampleResult)

	out := mustBfc(t, dir, "done", short, "-m", "Shipped in 1.4.")

	for _, want := range []string{
		"warning: T-1 is claimed, not completed", "warning: T-2 is open, not completed",
		"warning: pull request #7 has 2 Findings", short + "  done",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	e := last(t, dir, id)
	if e.Type != core.Done || e.Agent != "" || e.Message == "" || e.Author.Email != adaP.Email {
		t.Fatalf("event = %+v", e)
	}
	msg := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/messages/"+e.Message+".md")
	if !strings.HasSuffix(msg, "Shipped in 1.4.") {
		t.Errorf("message:\n%s", msg)
	}
	if got := state(t, dir, short); got != "done" {
		t.Errorf("state = %s", got)
	}
}

// Issue #111 AC-1: only an in-progress plan is done, and a done plan is
// final: every command that writes refuses it.
func TestADonePlanIsFinal(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	approved, _ := approvedTwoTasks(t, dir, "Approved only")
	if out, err := bfcIn(t, dir, "done", approved); err == nil || !strings.Contains(out, "only an in-progress plan") {
		t.Errorf("done of an approved plan: %v\n%s", err, out)
	}
	mustBfc(t, dir, "done", short)
	check := strings.TrimSpace(recordResultErr(t, dir, short))
	_ = check

	for _, args := range [][]string{
		{"done", short},
		{"close", short},
		{"claim", short, "T-2"},
		{"handoff", short, "T-1", "-m", "x"},
		{"ask", short, "--to", "ben@example.com", "-m", "x"},
		{"edit", short, "--stdin", "--force"},
		{"start", short},
	} {
		out, err := bfcStdin(t, dir, validPlan, args...)
		if err == nil || !strings.Contains(out, "done") {
			t.Errorf("bfc %s on a done plan: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// recordResultErr is bfc check record on a plan that refuses it; it
// returns the output and fails the test if the record succeeded.
func recordResultErr(t *testing.T, dir, short string) string {
	t.Helper()
	out, err := bfcStdin(t, dir, sampleResult, "check", "record", short, "--pr", "1", "--head", "h", "--base", "b")
	if err == nil || !strings.Contains(out, "done is final") {
		t.Errorf("check record on a done plan: %v\n%s", err, out)
	}
	return out
}

// Issue #111 AC-2: done is a Gate: it refuses inside an agent and bfc setup
// claude denies it.
func TestDoneIsAGate(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	setEnv(t, agentEnv, "1")
	if out, err := bfcIn(t, dir, "done", short); err == nil || !strings.Contains(out, "bfc done") {
		t.Errorf("inside an agent: %v\n%s", err, out)
	}
	mustBfc(t, dir, "setup", "claude")
	if settings := readFile(t, filepath.Join(dir, ".claude", "settings.json")); !strings.Contains(settings, `"Bash(bfc done:*)"`) {
		t.Errorf("settings do not deny done:\n%s", settings)
	}
}

// Issue #111 AC4: bfc list leaves done plans out unless asked for.
func TestListHidesDonePlans(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	other, _ := newPlan(t, dir, "Still open")
	mustBfc(t, dir, "done", short)

	if out := mustBfc(t, dir, "list"); strings.Contains(out, short) || !strings.Contains(out, other) {
		t.Errorf("bfc list:\n%s", out)
	}
	if out := mustBfc(t, dir, "list", "--state", "done"); !strings.Contains(out, short) || strings.Contains(out, other) {
		t.Errorf("bfc list --state done:\n%s", out)
	}
	if out := mustBfc(t, dir, "list", "--all"); !strings.Contains(out, short) {
		t.Errorf("bfc list --all:\n%s", out)
	}
}

// Issue #111 AC-5: the TUI adapter offers done only to a person while the
// plan is in-progress.
func TestTUIAdapterOffersDoneWhileInProgress(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	d, err := adapterIn(t, dir).Detail(t.Context(), short)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(d.Actions, tui.ActionDone) {
		t.Errorf("in-progress actions = %v", d.Actions)
	}
	approved, _ := approvedTwoTasks(t, dir, "Approved only")
	if d, _ := adapterIn(t, dir).Detail(t.Context(), approved); slices.Contains(d.Actions, tui.ActionDone) {
		t.Errorf("approved actions = %v", d.Actions)
	}
}
