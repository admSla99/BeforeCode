package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
		`"AC-1": "met | not-met | cannot-verify"`,
		`"out_of_scope": [`,
		`"summary": `,
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

// Issue #102 AC-4: a recorded agent result, the JSON fence at the end of
// the agent's last message, goes through bfc check record end to end.
func TestARecordedAgentResultIsRecorded(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	reply := readFile(t, filepath.Join("testdata", "plan-check-reply.md"))
	m := regexp.MustCompile("(?s)```json\n(.*?)\n```\\s*$").FindStringSubmatch(reply)
	if m == nil {
		t.Fatalf("the recorded reply does not end with a json fence:\n%s", reply)
	}
	var shape map[string]json.RawMessage
	if err := json.Unmarshal([]byte(m[1]), &shape); err != nil {
		t.Fatal(err)
	}

	out := strings.TrimSpace(mustStdin(t, dir, m[1], "check", "record", short, "--pr", "3", "--head", "9f2c", "--base", "1a7e"))

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
