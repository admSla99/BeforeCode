package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

func renderPlanTopicString(t *testing.T, v topicView) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderPlanTopic(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var (
	testPlanSkill    = []byte("---\nname: write-plan\ndescription: Write plan.md.\n---\n\n# Write plan.md\n\nUse the template.\n")
	testPlanTemplate = []byte("## Context\n\n## Acceptance criteria\n\n- AC-1:\n")
)

// AC-4: the skill, the plan and how to write plan.md, the template, the
// Intent and the Grill's Questions and answers.
func TestPrimePlanOfAGrilledDraft(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateDraft)
	p.Plan, p.Status.RequiredApprovals, p.Status.Approvers = nil, 0, nil
	asked := core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5A"), Author: adaP, At: primeAt, Agent: claudeCode,
		To: []string{"ada@example.com"}, Ref: "intent.md", Body: "Which cards fail?",
	}
	answer := core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5B"), Author: adaP, At: primeAt,
		ReplyTo: asked.ID.String(), Body: "Only 3-D Secure cards.",
	}
	golden(t, renderPlanTopicString(t, topicView{
		Skill: testPlanSkill, Template: testPlanTemplate, Plan: p, Current: true,
		Messages: []core.Message{asked, answer},
	}))
}

// AC-2, AC-4: a plan whose plan.md is written shows it and says that
// replacing it needs --force and the user's word.
func TestPrimePlanOfAWrittenPlan(t *testing.T) {
	t.Parallel()
	golden(t, renderPlanTopicString(t, topicView{
		Skill: testPlanSkill, Template: testPlanTemplate, Plan: paymentsPlan(core.StateInReview),
	}))
}

// AC-3, AC-4: bfc prime plan on a real plan: the Current plan without an
// ID, the Grill so far, and the repository's skill and template instead of
// the built-in ones.
func TestPrimePlanCommand(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	setEnv(t, agentEnv, "1")
	q := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Which cards fail?"))
	mustBfc(t, dir, "reply", q, "--verbatim", "-m", "Only 3-D Secure cards.")

	out := mustBfc(t, dir, "prime", "plan", short)
	builtIn := strings.SplitN(readFile(t, filepath.Join(repoSkills, "write-plan", "SKILL.md")), "---\n", 3)[2]
	for _, want := range []string{
		strings.TrimSpace(builtIn),
		"## Plan: " + short + " Payments",
		"bfc edit " + short + " --stdin --check",
		"## Acceptance criteria",
		"title: Payments",
		"Which cards fail?",
		"Only 3-D Secure cards.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	writeFile(t, filepath.Join(dir, ".beforecode", "skills", "write-plan", "SKILL.md"), "# Our own planning\n")
	writeFile(t, filepath.Join(dir, ".beforecode", "templates", "plan.md"), "## Our own template\n")
	mustBfc(t, dir, "use", short)
	out = mustBfc(t, dir, "prime", "plan")
	if !strings.HasPrefix(out, "# Our own planning\n") || !strings.Contains(out, "## Our own template") {
		t.Errorf("the repository's skill and template do not win:\n%s", out)
	}
	if !strings.Contains(out, "## Current plan: "+short) {
		t.Errorf("without an ID, the Current plan:\n%s", out)
	}
}

// AC-4: like bfc prime, never a failure over plans.
func TestPrimePlanCommandWithoutAPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	out := mustBfc(t, dir, "prime", "plan")
	if !strings.Contains(out, "# Write plan.md") || !strings.Contains(out, "There is no Current plan") || !strings.Contains(out, "bfc prime plan <id>") {
		t.Errorf("bfc prime plan:\n%s", out)
	}
}
