package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

// AC-1: --stdin writes plan.md without an editor and commits it; the same
// text again commits nothing.
func TestEditStdinWritesPlanMd(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	setEnv(t, agentEnv, "1") // agents use it; no editor may start
	fakeEditor(t, "exit 7")

	out, err := bfcStdin(t, dir, validPlan, "edit", short, "--stdin")
	if err != nil {
		t.Fatalf("bfc edit --stdin: %v\n%s", err, out)
	}
	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/plan.md"); got != strings.TrimSpace(validPlan) {
		t.Errorf("plan.md in the ref =\n%s", got)
	}
	if !strings.Contains(out, "saved plan.md of "+short) {
		t.Errorf("output:\n%s", out)
	}

	before := planCommits(t, dir)
	out = mustStdin(t, dir, strings.ReplaceAll(validPlan, "\n", "\r\n"), "edit", short, "--stdin", "--force")
	if !strings.Contains(out, "no changes to plan.md of "+short) || planCommits(t, dir) != before {
		t.Errorf("the same text (with CRLF) must commit nothing:\n%s", out)
	}
}

// AC-1: --intent with --stdin writes intent.md, checked like bfc edit.
func TestEditStdinWritesTheIntent(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	intent := "---\ntitle: Payments via Stripe\n---\n\n## Problem\n\nCards fail.\n"

	mustStdin(t, dir, intent, "edit", short, "--stdin", "--intent")
	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/intent.md"); got != strings.TrimSpace(intent) {
		t.Errorf("intent.md in the ref =\n%s", got)
	}
	before := planCommits(t, dir)
	if out, err := bfcStdin(t, dir, "## Problem\n\nNo front matter.\n", "edit", short, "--stdin", "--intent"); err == nil || !strings.Contains(out, "intent.md") {
		t.Errorf("an intent without front matter must be refused, got %v:\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Error("a refused intent was committed")
	}
}

// AC-1: an empty document is refused; problems in plan.md are warnings the
// agent can fix, and the text is committed.
func TestEditStdinRefusesEmptyAndWarns(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	before := planCommits(t, dir)

	out, err := bfcStdin(t, dir, " \n\n", "edit", short, "--stdin")
	if err == nil || !strings.Contains(out, "plan.md is empty") || planCommits(t, dir) != before {
		t.Errorf("an empty plan.md must be refused, got %v:\n%s", err, out)
	}
	if strings.Contains(out, "kept in") {
		t.Errorf("stdin keeps no file; the agent still has its text:\n%s", out)
	}

	out = mustStdin(t, dir, "## Context\n\nOnly context.\n", "edit", short, "--stdin")
	if !strings.Contains(out, "warning: plan.md") || !strings.Contains(out, "saved plan.md") {
		t.Errorf("want the warnings and the save:\n%s", out)
	}
}

// AC-2: --stdin replaces plan.md only while it is missing or still the
// template; a written plan.md needs --force.
func TestEditStdinRefusesToReplaceAWrittenPlan(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	before := planCommits(t, dir)
	other := strings.Replace(validPlan, "Stripe.", "Adyen.", 1)

	out, err := bfcStdin(t, dir, other, "edit", short, "--stdin")
	if err == nil || !strings.Contains(out, "--force") || planCommits(t, dir) != before {
		t.Errorf("want a refusal naming --force, got %v:\n%s", err, out)
	}

	mustStdin(t, dir, other, "edit", short, "--stdin", "--force")
	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/plan.md"); !strings.Contains(got, "Adyen.") {
		t.Errorf("--force did not replace plan.md:\n%s", got)
	}

	// The template itself may be replaced without --force.
	short2, id2 := newPlan(t, dir, "Refunds")
	writeRef(t, dir, "template", map[string][]byte{core.PlanDir(id2) + "/plan.md": []byte(readFile(t, "../../.beforecode/templates/plan.md"))})
	mustStdin(t, dir, validPlan, "edit", short2, "--stdin")
}

// --force belongs to --stdin.
func TestEditForceNeedsStdin(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	if out, err := bfcIn(t, dir, "edit", short, "--force"); err == nil || !strings.Contains(out, "--stdin") {
		t.Errorf("want an error naming --stdin, got %v:\n%s", err, out)
	}
}

// AC-1: the concurrent-change check: when the document changes in the plans
// ref while stdin is read, nothing is committed.
func TestEditStdinNoticesAConcurrentChange(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	in := &changingReader{r: strings.NewReader(validPlan), change: func() {
		writeRef(t, dir, "meanwhile", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte("## Context\n\nSomeone else.\n")})
	}}
	var out bytes.Buffer
	cmd := newRootCmd("dev")
	cmd.SetIn(in)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"edit", short, "--stdin"})
	err := cmd.ExecuteContext(inPlace(t, dir))
	if err == nil || !strings.Contains(out.String(), "changed in the plans ref") {
		t.Errorf("want the concurrent change refused, got %v:\n%s", err, out.String())
	}
	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/plan.md"); got != "## Context\n\nSomeone else." {
		t.Errorf("the other change was overwritten:\n%s", got)
	}
}

// changingReader calls change once, on the first Read: another writer
// changes the plans ref while bfc reads stdin.
type changingReader struct {
	r      io.Reader
	change func()
	done   bool
}

func (c *changingReader) Read(p []byte) (int, error) {
	if !c.done {
		c.done = true
		c.change()
	}
	return c.r.Read(p)
}

func mustStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	out, err := bfcStdin(t, dir, stdin, args...)
	if err != nil {
		t.Fatalf("bfc %v: %v\n%s", args, err, out)
	}
	return out
}
