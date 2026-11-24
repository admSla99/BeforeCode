package main

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/agent/claude"
	"github.com/admSla99/BeforeCode/internal/core"
)

func renderCheckTopicString(t *testing.T, v topicView) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderCheckTopic(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var testCheckSkill = []byte("---\nname: plan-check\ndescription: Check a pull request.\n---\n\n# Check a pull request\n\nRead only.\n")

// checkFixturePlan has two criteria, one with a continuation line, and an
// Out of scope section.
const checkFixturePlan = `## Context

Cards fail.

## Approach

Stripe.

## Affected areas

payments/

## Out of scope

Refunds and currencies other than EUR.

## Tasks

- T-1: Add the client.

## Acceptance criteria

- AC-1: An invalid card returns 402.
- AC-2: A successful payment sends one email,
  even when the webhook is delivered twice.
`

// Issue #102 AC-3, AC-4: the skill without front matter, the plan, its
// criteria with their text, Out of scope, the whole plan.md and the exact
// result shape.
func TestPrimeCheckOfAPlan(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateApproved)
	p.Plan = []byte(checkFixturePlan)
	out := renderCheckTopicString(t, topicView{Skill: testCheckSkill, Plan: p})
	golden(t, out)
	for _, want := range []string{
		"# Check a pull request\n\nRead only.\n",
		"## Plan: bfc-5v7x Payments via Stripe",
		"- State: approved",
		"- AC-1: An invalid card returns 402.",
		"- AC-2: A successful payment sends one email,\n  even when the webhook is delivered twice.",
		"Refunds and currencies other than EUR.",
		`"criteria": {`,
		`"AC-1": "<status>"`,
		"each `met`, `not-met` or `cannot-verify`",
		`"out_of_scope": [`,
		`"summary": `,
		"`bfc check record bfc-5v7x --pr <n> --head <head sha> --base <base sha>`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "name: plan-check") {
		t.Errorf("the skill's front matter is in the prompt")
	}
}

// Issue #102 AC-3: without plan.md there is nothing to check against, and
// a very long plan.md is cut to stay within 8 kB.
func TestPrimeCheckWithoutPlanMdAndWithALongOne(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateDraft)
	p.Plan = nil
	if out := renderCheckTopicString(t, topicView{Skill: testCheckSkill, Plan: p}); !strings.Contains(out, "has no plan.md") {
		t.Errorf("no plan.md:\n%s", out)
	}

	p = paymentsPlan(core.StateApproved)
	p.Plan = []byte(checkFixturePlan + strings.Repeat("More context that goes on and on.\n", 400))
	out := renderCheckTopicString(t, topicView{Skill: testCheckSkill, Plan: p})
	if len(out) > primeLimit {
		t.Errorf("output is %d bytes, more than %d", len(out), primeLimit)
	}
	for _, want := range []string{"plan.md is cut here; run `bfc show bfc-5v7x` for all of it.", `"summary": `, "- AC-2:"} {
		if !strings.Contains(out, want) {
			t.Errorf("long plan.md: missing %q", want)
		}
	}
}

// Issue #102 AC-1, AC-3: bfc prime check on a real plan uses the built-in
// skill, then the repository's copy once there is one, and never fails.
func TestPrimeCheckCommand(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)

	out := mustBfc(t, dir, "prime", "check", short)
	builtIn := strings.SplitN(readFile(t, filepath.Join(repoSkills, "plan-check", "SKILL.md")), "---\n", 3)[2]
	if !strings.Contains(out, strings.TrimSpace(builtIn)) || !strings.Contains(out, "## Plan: "+short+" Payments") {
		t.Errorf("prime check:\n%s", out)
	}
	writeFile(t, filepath.Join(dir, ".beforecode", "skills", "plan-check", "SKILL.md"), "---\nname: plan-check\n---\n\nOur own check.\n")
	if out := mustBfc(t, dir, "prime", "check", short); !strings.Contains(out, "Our own check.") {
		t.Errorf("the repository's skill does not win:\n%s", out)
	}
	if out := mustBfc(t, dir, "prime", "check", "bfc-none"); !strings.Contains(out, "bfc prime check <id>") {
		t.Errorf("unknown plan:\n%s", out)
	}
}

// Issue #102 AC-4: a recorded agent reply, ending with the result in a
// json fence, goes through bfc check record end to end: record takes the
// last json fence of the reply.
func TestARecordedAgentResultIsRecorded(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	reply := readFile(t, filepath.Join("testdata", "plan-check-reply.md"))

	out := strings.TrimSpace(mustStdin(t, dir, reply, "check", "record", short, "--pr", "3", "--head", "9f2c", "--base", "1a7e"))

	show := mustBfc(t, dir, "show", short)
	for _, want := range []string{"PR #3  " + out, "out of scope: Adds a retry loop to `mail/worker.go`."} {
		if !strings.Contains(show, want) {
			t.Errorf("bfc show lacks %q:\n%s", want, show)
		}
	}
	if got := gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(id)+"/checks"); got != out+".md" {
		t.Errorf("checks/ = %q", got)
	}
}

// A reply without a json fence, or whose last one is not the result, is
// refused.
func TestCheckRecordNeedsTheResultAtTheEnd(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	for name, reply := range map[string]string{
		"no fence":         "All criteria met.\n",
		"broken last json": "```json\n{\"criteria\": {\"AC-1\": \"met\"}, \"summary\": \"x\"}\n```\n\n```json\n{oops\n```\n",
	} {
		if out, err := bfcStdin(t, dir, reply, "check", "record", short, "--pr", "1", "--head", "h", "--base", "b"); err == nil {
			t.Errorf("%s: recorded:\n%s", name, out)
		}
	}
}

// Issue #102 AC-3: the output stays within 8 kB even when the skill, the
// criteria and Out of scope alone are long.
func TestPrimeCheckStaysWithin8kB(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateApproved)
	var criteria strings.Builder
	for i := 1; i <= 120; i++ {
		criteria.WriteString("- AC-" + strconv.Itoa(i) + ": " + strings.Repeat("The API answers within the time limit. ", 2) + "\n")
	}
	p.Plan = []byte(strings.Replace(checkFixturePlan, "- AC-1: An invalid card returns 402.\n", criteria.String(), 1))
	out := renderCheckTopicString(t, topicView{Skill: testCheckSkill, Plan: p})
	if len(out) > primeLimit {
		t.Errorf("output is %d bytes, more than %d", len(out), primeLimit)
	}
	if !strings.Contains(out, "is cut here; run `bfc show bfc-5v7x` for all of it.") {
		t.Errorf("no cut note")
	}
}

// Issue #138 AC-2, AC-3: a request to check a pull request against its plan
// leads the agent to bfc prime check, whose skill walks the whole local
// run, from the pull request to bfc push.
func TestTheLocalPlanCheckIsInTheInstructions(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"bfc prime":        primeInstructions,
		"beforecode skill": string(claude.Skill),
	} {
		if !strings.Contains(text, "against its plan") || !strings.Contains(text, "`bfc prime check`") {
			t.Errorf("%s does not lead a plan check to bfc prime check:\n%s", name, text)
		}
	}
	skill := readFile(t, filepath.Join(repoSkills, "plan-check", "SKILL.md"))
	for _, want := range []string{"gh pr view", "git fetch origin", "bfc pull", "bfc check find --branch", "--body-file -", "bfc prime check <id>", "bfc check record", "<<'EOF'", "bfc push", "Inbox", "gh pr comment"} {
		if !strings.Contains(skill, want) {
			t.Errorf("the plan-check skill lacks %q", want)
		}
	}
}

// Issue #138 AC-2: a pull request is checked against its own plan, so
// without an ID bfc prime check asks for it instead of taking the Current
// plan.
func TestPrimeCheckWithoutIDNamesNoPlan(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "use", short)
	out := mustBfc(t, dir, "prime", "check")
	if strings.Contains(out, "## Current plan") || strings.Contains(out, "bfc list") || !strings.Contains(out, "bfc check find") || !strings.Contains(out, "bfc prime check <id>") {
		t.Errorf("prime check without an ID:\n%s", out)
	}
}

// Issue #138: without the Plans ref, bfc prime check without an ID still
// says how to get them, before the agent runs bfc check find.
func TestPrimeCheckWithoutIDKeepsTheSetupNote(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	if out := mustBfc(t, dir, "prime", "check"); !strings.Contains(out, "bfc init") && !strings.Contains(out, "bfc bootstrap") {
		t.Errorf("no setup note:\n%s", out)
	}
}
