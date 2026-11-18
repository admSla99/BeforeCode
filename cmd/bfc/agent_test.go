package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/tui"
)

// AC-1: inside an agent every Gate exits non-zero, says the action is for a
// person, and writes nothing.
func TestGatesRefuseToRunInsideAnAgent(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	setEnv(t, "CLAUDECODE", "1")
	gates := [][]string{
		{"submit", short},
		{"approve", short},
		{"request-changes", short, "-m", "Split AC-1."},
		{"withdraw", short},
		{"close", short, "-m", "Not needed."},
	}
	for _, args := range gates {
		before := gitIn(t, dir, "rev-parse", plansRef)
		out, err := bfcIn(t, dir, args...)
		if err == nil || !strings.Contains(out, "for a person") {
			t.Errorf("bfc %s inside an agent: err %v\n%s", args[0], err, out)
		}
		if gitIn(t, dir, "rev-parse", plansRef) != before {
			t.Errorf("bfc %s inside an agent wrote to the Plans ref", args[0])
		}
	}
}

// AC-4: every other command keeps working inside an agent.
func TestOtherCommandsWorkInsideAnAgent(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	setEnv(t, "CLAUDECODE", "1")
	mustBfc(t, dir, "list")
	mustBfc(t, dir, "show", short)
	mustBfc(t, dir, "new", "Agent's plan", "--no-edit")
	mustBfc(t, dir, "ask", short, "--to", "ai", "-m", "Why?")
	mustBfc(t, dir, "doctor")
}

// AC-5: an empty variable is no agent; the Gates work again.
func TestGatesRunWhenTheAgentVariableIsEmpty(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	setEnv(t, "CLAUDECODE", "1")
	if _, err := bfcIn(t, dir, "submit", short); err == nil {
		t.Fatal("submit inside an agent succeeded")
	}
	setEnv(t, "CLAUDECODE", "")
	mustBfc(t, dir, "submit", short)
}

// AC-3: the TUI hides the Gate actions inside an agent.
func TestTUIAdapterHidesTheGatesInsideAnAgent(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	setEnv(t, "CLAUDECODE", "1")
	d, err := reviewAdapter(t, dir).Detail(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Actions) != 0 || !d.Editable {
		t.Errorf("actions inside an agent = %v (editable %v), want none and editable", d.Actions, d.Editable)
	}
	if err := reviewAdapter(t, dir).Review(context.Background(), short, tui.ActionApprove, ""); err == nil || !strings.Contains(err.Error(), "for a person") {
		t.Errorf("Review inside an agent: %v", err)
	}
	setEnv(t, "CLAUDECODE", "")
	d, _ = reviewAdapter(t, dir).Detail(context.Background(), short)
	if !slices.Contains(d.Actions, tui.ActionApprove) {
		t.Errorf("actions outside an agent = %v, want approve", d.Actions)
	}
}

// AC-2: every name in the one list of Gates is a command, so renaming a
// Gate cannot silently drop its guard.
func TestEveryGateIsACommand(t *testing.T) {
	t.Parallel()
	root := newRootCmd("dev")
	for _, name := range gateCommands {
		c, _, err := root.Find([]string{name})
		if err != nil || c == root || c.Name() != name || c.PreRunE == nil {
			t.Errorf("gate %q: command %v, err %v", name, c, err)
		}
	}
}
