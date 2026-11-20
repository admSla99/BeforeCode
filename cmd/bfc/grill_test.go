package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

func renderGrillString(t *testing.T, v topicView) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderGrill(&b, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

var testGrillSkill = []byte("---\nname: grill-intent\ndescription: Grill an Intent.\n---\n\n# Grill an Intent\n\nAsk one question per round.\n")

// AC-4: the skill without its front matter, the plan with its author and
// how to record a round, its Intent, then each Question with its answers.
func TestPrimeGrillOfAPlanWithAnsweredAndOpenQuestions(t *testing.T) {
	t.Parallel()
	p := paymentsPlan(core.StateDraft)
	p.Plan, p.Status.RequiredApprovals, p.Status.Approvers = nil, 0, nil
	asked := core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5A"), Author: adaP, At: primeAt, Agent: claudeCode,
		To: []string{"ada@example.com"}, Ref: "intent.md", Body: "Which cards fail?\nAll of them, or some?",
	}
	answer := core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5B"), Author: adaP, At: primeAt,
		ReplyTo: asked.ID.String(), Body: "Only 3-D Secure cards.",
	}
	open := core.Message{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5C"), Author: adaP, At: primeAt, Agent: claudeCode,
		To: []string{"ada@example.com"}, Ref: "intent.md", Body: "How do we know it worked?",
	}
	chat := core.Message{ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C5D"), Author: benP, At: primeAt, Body: "Nice idea."}
	golden(t, renderGrillString(t, topicView{Skill: testGrillSkill, Plan: p, Current: true, Messages: []core.Message{asked, answer, open, chat}}))
}

// AC-4: without a plan, the skill and how to name one.
func TestPrimeGrillWithoutAPlan(t *testing.T) {
	t.Parallel()
	golden(t, renderGrillString(t, topicView{Skill: testGrillSkill, Note: "There is no Current plan."}))
}

// AC-3, AC-4: bfc prime grill runs on a real plan: the Current plan without
// an ID, the Questions asked so far, and the repository's skill instead of
// the built-in one.
func TestPrimeGrillCommand(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	setEnv(t, agentEnv, "1")
	q := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--ref", "intent.md", "-m", "Which cards fail?"))
	mustBfc(t, dir, "reply", q, "--verbatim", "-m", "Only 3-D Secure cards.")

	out := mustBfc(t, dir, "prime", "grill", short)
	builtIn := strings.SplitN(readFile(t, filepath.Join(repoSkills, "grill-intent", "SKILL.md")), "---\n", 3)[2]
	for _, want := range []string{
		strings.TrimSpace(builtIn),
		"## Plan: " + short + " Payments",
		"--to ada@example.com",
		"title: Payments",
		"Question " + q,
		"Which cards fail?",
		"Only 3-D Secure cards.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	writeFile(t, filepath.Join(dir, ".beforecode", "skills", "grill-intent", "SKILL.md"), "# Our own grill\n")
	mustBfc(t, dir, "use", short)
	out = mustBfc(t, dir, "prime", "grill")
	if !strings.HasPrefix(out, "# Our own grill\n") || strings.Contains(out, "# Grill an Intent") {
		t.Errorf("the repository's skill does not win:\n%s", out)
	}
	if !strings.Contains(out, "## Current plan: "+short) {
		t.Errorf("without an ID, the Current plan:\n%s", out)
	}
}

// AC-4: bfc prime grill never fails over plans, like bfc prime.
func TestPrimeGrillCommandWithoutAPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	out := mustBfc(t, dir, "prime", "grill")
	if !strings.Contains(out, "# Grill an Intent") || !strings.Contains(out, "There is no Current plan") || !strings.Contains(out, "bfc prime grill <id>") {
		t.Errorf("bfc prime grill:\n%s", out)
	}
	if out := mustBfc(t, dir, "prime", "grill", "bfc-zzzz"); !strings.Contains(out, `no plan "bfc-zzzz"`) {
		t.Errorf("unknown plan:\n%s", out)
	}
}

// The front matter is for the harness; the agent gets the body, also from
// a file with CRLF, a byte order mark or no newline after the front matter.
func TestWithoutFrontMatter(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"---\nname: x\n---\n\n# Body\n":       "# Body\n",
		"---\r\nname: x\r\n---\r\n# Body\r\n": "# Body\n",
		"\ufeff---\nname: x\n---\n# Body\n":   "# Body\n",
		"---\nname: x\n---":                   "",
		"# No front matter\n":                 "# No front matter\n",
	} {
		if got := withoutFrontMatter(in); got != want {
			t.Errorf("withoutFrontMatter(%q) = %q, want %q", in, got, want)
		}
	}
}

// Like bfc prime, bfc prime grill reads only the local Plans ref: without
// one it says how to set BeforeCode up, and still prints the skill.
func TestPrimeGrillCommandWithoutAPlansRef(t *testing.T) {
	t.Parallel()
	plain := t.TempDir()
	gitIn(t, plain, "init", "-q")
	gitIn(t, plain, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	out := mustBfc(t, plain, "prime", "grill")
	if !strings.Contains(out, "# Grill an Intent") || !strings.Contains(out, "BeforeCode is not set up") {
		t.Errorf("bfc prime grill without a plans ref:\n%s", out)
	}
}
