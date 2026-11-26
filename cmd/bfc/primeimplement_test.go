package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

func renderImplementString(t *testing.T, v implementView) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderImplementTopic(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var testImplementSkill = []byte("---\nname: implement-task\ndescription: Implement one Task.\n---\n\n# Implement one Task\n\nTests first.\n")

// implementFixturePlan has two Tasks, one with a continuation line, and two
// criteria.
const implementFixturePlan = `## Context

Cards fail.

## Approach

Stripe.

## Affected areas

payments/

## Out of scope

Refunds.

## Tasks

- T-1: Add the client.
- T-2: Wire the flag,
  behind the setting.

## Acceptance criteria

- AC-1: An invalid card returns 402.
- AC-2: A successful payment sends one email,
  even when the webhook is delivered twice.
`

// implementPlan is the payments plan in state with implementFixturePlan and
// the given Task states.
func implementPlan(state core.State, tasks map[string]core.TaskStatus) implementView {
	p := paymentsPlan(state)
	p.Plan = []byte(implementFixturePlan)
	p.Status.Tasks = tasks
	return implementView{topicView: topicView{Skill: testImplementSkill, Plan: p, Current: true}, Me: adaP}
}

func handoffFor(task, note string, done bool, at int64) core.Handoff {
	return core.Handoff{
		ID: ulid.MustNew(uint64(at), bytes.NewReader(make([]byte, 16))), Task: task, Author: adaP,
		At: time.UnixMilli(at), Done: done, Note: note,
	}
}

// bfc-nxpr AC-1, AC-2: the skill without front matter, the plan with its
// state, the given Task with its text, state and holder, the criteria with
// their text, plan.md and only that Task's Handoffs, oldest first.
func TestPrimeImplementOfAGivenTask(t *testing.T) {
	t.Parallel()
	v := implementPlan(core.StateInProgress, map[string]core.TaskStatus{
		"T-1": {State: core.TaskClaimed, Claim: core.Claim{Holder: janaP}},
		"T-2": {State: core.TaskPaused, Claim: core.Claim{Holder: adaP}},
	})
	v.Task = "T-2"
	v.Handoffs = []core.Handoff{
		handoffFor("T-2", "First half.\n\n## Left\nthe setting", false, 1000),
		handoffFor("T-1", "Not this one.", false, 1500),
		handoffFor("T-2", "Second try.", false, 2000),
	}
	out := renderImplementString(t, v)
	golden(t, out)
	for _, want := range []string{
		"# Implement one Task\n\nTests first.\n",
		"## Current plan: bfc-5v7x Payments via Stripe",
		"- State: in-progress",
		"## Task T-2",
		"you named it",
		"- State: paused, last held by ada@example.com",
		"Wire the flag,\nbehind the setting.",
		"`bfc claim bfc-5v7x T-2 --json`",
		"`bfc handoff bfc-5v7x T-2",
		"--to ada@example.com",
		"- AC-1: An invalid card returns 402.",
		"- AC-2: A successful payment sends one email,\n  even when the webhook is delivered twice.",
		"### plan.md",
		"## Left\nthe setting",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "name: implement-task") {
		t.Error("the skill's front matter is in the output")
	}
	if strings.Contains(out, "Not this one.") {
		t.Error("a Handoff of another Task is in the output")
	}
	if i, j := strings.Index(out, "First half."), strings.Index(out, "Second try."); i < 0 || j < i {
		t.Error("the Handoffs are not oldest first")
	}
}

// bfc-nxpr AC-3: without T-n, the Task the git author holds, else the one
// bfc claim would take, and the output says which rule chose it.
func TestPrimeImplementChoosesTheTask(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		tasks map[string]core.TaskStatus
		task  string
		why   string
	}{
		{"holds T-2", map[string]core.TaskStatus{
			"T-1": {State: core.TaskClaimed, Claim: core.Claim{Holder: janaP}},
			"T-2": {State: core.TaskClaimed, Claim: core.Claim{Holder: core.Person{Email: "ADA@example.com"}}},
		}, "T-2", "you hold it"},
		{"first open", map[string]core.TaskStatus{
			"T-1": {State: core.TaskPaused, Claim: core.Claim{Holder: janaP}},
		}, "T-2", "the next one `bfc claim` would take"},
		{"then first Paused", map[string]core.TaskStatus{
			"T-1": {State: core.TaskPaused, Claim: core.Claim{Holder: janaP}},
			"T-2": {State: core.TaskClaimed, Claim: core.Claim{Holder: janaP}},
		}, "T-1", "the next one `bfc claim` would take"},
	} {
		out := renderImplementString(t, implementPlan(core.StateInProgress, c.tasks))
		if !strings.Contains(out, "## Task "+c.task+"\n") || !strings.Contains(out, c.why) {
			t.Errorf("%s: want %s because %s:\n%s", c.name, c.task, c.why, out)
		}
	}
}

// bfc-nxpr AC-5: a note and no Task for a plan that is not approved or
// in-progress, an unknown Task, a plan without Tasks and one whose Tasks
// are all completed.
func TestPrimeImplementNotes(t *testing.T) {
	t.Parallel()
	draft := implementPlan(core.StateDraft, nil)
	unknown := implementPlan(core.StateApproved, nil)
	unknown.Task = "T-9"
	noTasks := implementPlan(core.StateApproved, nil)
	noTasks.Plan.Plan = []byte(checkFixturePlan[:strings.Index(checkFixturePlan, "## Tasks")])
	done := implementPlan(core.StateInProgress, map[string]core.TaskStatus{
		"T-1": {State: core.TaskCompleted, Claim: core.Claim{Holder: adaP}},
		"T-2": {State: core.TaskCompleted, Claim: core.Claim{Holder: adaP}},
	})
	for name, c := range map[string]struct {
		v    implementView
		want []string
	}{
		"draft":    {draft, []string{"draft", "not approved or in-progress", "Gate"}},
		"unknown":  {unknown, []string{"T-9 is not a Task of bfc-5v7x", "- T-1: Add the client.", "- T-2: Wire the flag,"}},
		"no tasks": {noTasks, []string{"bfc-5v7x has no Tasks"}},
		"done":     {done, []string{"Every Task of bfc-5v7x is completed", "After the last Task"}},
	} {
		out := renderImplementString(t, c.v)
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q:\n%s", name, want, out)
			}
		}
		if strings.Contains(out, "## Task ") || strings.Contains(out, "### Acceptance criteria") {
			t.Errorf("%s: shows a Task:\n%s", name, out)
		}
	}
}

// bfc-nxpr AC-6: at most 8 kB; plan.md is cut with a note, and the skill,
// the Task, the criteria and the Handoffs stay whole.
func TestPrimeImplementStaysWithin8kB(t *testing.T) {
	t.Parallel()
	v := implementPlan(core.StateInProgress, nil)
	v.Plan.Plan = []byte(implementFixturePlan + strings.Repeat("Filler line of the approach.\n", 400))
	long := strings.Repeat("A long Handoff line.\n", 40)
	v.Handoffs = []core.Handoff{handoffFor("T-1", long+"The end of the Handoff.", false, 1000)}
	out := renderImplementString(t, v)
	if len(out) > primeLimit {
		t.Errorf("output is %d bytes", len(out))
	}
	for _, want := range []string{
		"# Implement one Task\n\nTests first.\n", "## Task T-1", "- AC-2: A successful payment sends one email,\n  even when the webhook is delivered twice.",
		"The end of the Handoff.", "plan.md is cut here; run `bfc show bfc-5v7x`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// bfc-nxpr AC-1 to AC-4: against a repository with Claims and Handoffs;
// the repository's skill wins; T-n alone names a Task of the Current plan;
// more than two arguments are an error; without a Current plan it says to
// name a plan.
func TestPrimeImplementCommand(t *testing.T) {
	t.Parallel()
	dir, short, _ := claimedPlan(t)
	mustBfc(t, dir, "handoff", short, "T-1", "-m", "Half done.\n\n## Left\nThe retries.")
	mustBfc(t, dir, "claim", short, "T-1")

	out := mustBfc(t, dir, "prime", "implement")
	if !strings.Contains(out, "Name the plan") || !strings.Contains(out, "`bfc start <id>` only when the user asks") {
		t.Errorf("without a Current plan:\n%s", out)
	}

	out = mustBfc(t, dir, "prime", "implement", short)
	for _, want := range []string{"# Implement one Task", "## Task T-1", "you hold it", "Half done.\n\n## Left\nThe retries."} {
		if !strings.Contains(out, want) {
			t.Errorf("bfc prime implement %s lacks %q:\n%s", short, want, out)
		}
	}

	writeFile(t, filepath.Join(dir, ".beforecode", "skills", "implement-task", "SKILL.md"), "---\nname: implement-task\n---\n\n# Our own way\n")
	mustBfc(t, dir, "use", short)
	out = mustBfc(t, dir, "prime", "implement", "t-2")
	if !strings.HasPrefix(out, "# Our own way\n") || !strings.Contains(out, "## Task T-2") || !strings.Contains(out, "you named it") {
		t.Errorf("bfc prime implement t-2:\n%s", out)
	}

	if _, err := bfcIn(t, dir, "prime", "implement", short, "T-1", "T-2"); err == nil {
		t.Error("three arguments after implement are accepted")
	}
}
