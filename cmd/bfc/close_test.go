package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

// #49 AC-1, AC-2: close writes a closed event, with the reason as a
// Message when there is one, and the plan is closed from any state.
func TestCloseWritesAClosedEventWithAnOptionalReason(t *testing.T) {
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)

	as(t, dir, benP) // not the plan author
	out := mustBfc(t, dir, "close", short, "-m", "Done outside BeforeCode.")
	if !strings.Contains(out, short+"  closed") {
		t.Errorf("close: %s", out)
	}
	e := last(t, dir, id)
	if e.Type != core.Closed || e.Author.Email != benP.Email || e.PlanBlob != "" || e.Message == "" {
		t.Fatalf("event = %+v", e)
	}
	msg := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/messages/"+e.Message+".md")
	if !strings.Contains(msg, "email: ben@example.com") || !strings.HasSuffix(msg, "Done outside BeforeCode.") {
		t.Errorf("message file:\n%s", msg)
	}
	if got := state(t, dir, short); got != "closed" {
		t.Errorf("state = %s", got)
	}

	other, otherID := newPlan(t, dir, "Never mind")
	mustBfc(t, dir, "close", other)
	if e := last(t, dir, otherID); e.Type != core.Closed || e.Message != "" {
		t.Errorf("close without -m: event = %+v", e)
	}
}

// #49 AC-3, AC-4: closed is final; every other command refuses and names
// the state.
func TestAClosedPlanRefusesEveryCommand(t *testing.T) {
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "close", short)

	for _, args := range [][]string{
		{"close", short},
		{"submit", short},
		{"approve", short},
		{"request-changes", short, "-m", "x"},
		{"withdraw", short},
		{"edit", short},
	} {
		if out, err := bfcIn(t, dir, args...); err == nil || !strings.Contains(out, short+" is closed") {
			t.Errorf("bfc %s on a closed plan: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// #49 AC-4: bfc list hides closed plans unless asked for them.
func TestListHidesClosedPlansByDefault(t *testing.T) {
	dir := newLocalRepo(t)
	open, _ := newPlan(t, dir, "Still open")
	closed, _ := newPlan(t, dir, "Closed one")
	mustBfc(t, dir, "close", closed)

	shorts := func(args ...string) string {
		var got []string
		for _, line := range strings.Split(strings.TrimSpace(mustBfc(t, dir, args...)), "\n") {
			if f := strings.Fields(line); len(f) > 0 {
				got = append(got, f[0])
			}
		}
		return strings.Join(got, " ")
	}
	if got := shorts("list"); got != open {
		t.Errorf("list = %q, want only %s", got, open)
	}
	if got := shorts("list", "--state", "closed"); got != closed {
		t.Errorf("list --state closed = %q, want %s", got, closed)
	}
	if got := shorts("list", "--all"); got != closed+" "+open {
		t.Errorf("list --all = %q, want both, newest first", got)
	}

	var plans []planJSON
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "list", "--json")), &plans); err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].ShortID != open {
		t.Errorf("list --json = %+v, want only %s", plans, open)
	}
	if out, err := bfcIn(t, dir, "list", "--all", "--state", "draft"); err == nil {
		t.Errorf("--all with --state succeeded:\n%s", out)
	}
}
