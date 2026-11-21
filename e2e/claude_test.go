package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file proves the phase 2 exit criterion of docs/design.md (issue
// #67): a developer goes from an Intent through a Grill to an approved plan,
// and Claude Code, set up with bfc setup claude, implements it while the
// hook blocks edits without an approved plan.
//
// Claude Code is the fake of the agent adapter (internal/agent/claude/
// testdata/fakeclaude): it runs the bfc commands a real Claude Code would run
// as tools, with CLAUDECODE set as Claude Code sets it, and replays a
// recorded output. The hook gets PreToolUse inputs recorded from a real
// Claude Code (testdata/README.md). The TUI Grill screen, which starts the
// agent and stores the answers, needs a terminal and has its own tests in
// internal/tui and cmd/bfc; here the person's answer goes through bfc reply,
// which writes the same Message. docs/e2e/manual-run-2026-11-20.md is the
// run through the TUI with a real Claude Code.

// claude runs the fake Claude Code in the developer's clone. Its tool is
// the shell command script, run with CLAUDECODE=1 like every command Claude
// Code runs; the output is the recorded hello.jsonl.
func (d *dev) claude(script string) {
	d.t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "internal", "agent", "claude", "testdata", "hello.jsonl"))
	if err != nil {
		d.t.Fatal(err)
	}
	r := d.run("", []string{"CLAUDECODE=1", "FAKECLAUDE_EXEC=" + script, "FAKECLAUDE_FIXTURE=" + fixture}, fakeClaude, "-p", "--output-format", "stream-json", "--verbose")
	if r.err != nil {
		d.t.Fatalf("%s: claude ran %q: %v\n%s%s", d.name, script, r.err, r.stdout, r.stderr)
	}
}

// agentBfc runs bfc in the developer's clone as Claude Code runs it, with
// CLAUDECODE=1 and stdin.
func (d *dev) agentBfc(stdin string, args ...string) result {
	d.t.Helper()
	return d.run(stdin, []string{"CLAUDECODE=1"}, bfcBin, args...)
}

// hook runs bfc hook pre-tool-use, as the settings that bfc setup claude
// wrote tell Claude Code to, with the recorded PreToolUse input of tool in
// testdata, its paths moved into the clone. It returns the reason of a
// denial, "" when the edit is allowed.
func (d *dev) hook(tool string) string {
	d.t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pretooluse-"+strings.ToLower(tool)+".json"))
	if err != nil {
		d.t.Fatal(err)
	}
	var in map[string]any
	if err := json.Unmarshal(data, &in); err != nil {
		d.t.Fatal(err)
	}
	input, _ := in["tool_input"].(map[string]any)
	path, _ := input["file_path"].(string)
	rel, ok := strings.CutPrefix(path, "/work/")
	if !ok {
		d.t.Fatalf("testdata: the %s input has no file_path under /work: %s", tool, data)
	}
	in["cwd"] = d.dir
	input["file_path"] = filepath.Join(d.dir, filepath.FromSlash(rel))
	stdin, err := json.Marshal(in)
	if err != nil {
		d.t.Fatal(err)
	}
	r := d.agentBfc(string(stdin), "hook", "pre-tool-use")
	if r.err != nil {
		d.t.Fatalf("%s: bfc hook pre-tool-use for %s: %v\n%s", d.name, tool, r.err, r.stderr)
	}
	if strings.TrimSpace(r.stdout) == "" {
		return ""
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName, PermissionDecision, PermissionDecisionReason string
		}
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		d.t.Fatalf("%s: hook output %q: %v", d.name, r.stdout, err)
	}
	h := out.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || h.PermissionDecisionReason == "" {
		d.t.Fatalf("%s: hook output = %s", d.name, r.stdout)
	}
	return h.PermissionDecisionReason
}

// blocked fails unless the hook denies both recorded edits with a reason
// that holds every one of want.
func (d *dev) blocked(when string, want ...string) {
	d.t.Helper()
	for _, tool := range []string{"Edit", "Write"} {
		reason := d.hook(tool)
		if reason == "" {
			d.t.Errorf("%s: %s: the hook allowed %s", d.name, when, tool)
			continue
		}
		for _, w := range want {
			if !strings.Contains(reason, w) {
				d.t.Errorf("%s: %s: the reason for %s lacks %q: %s", d.name, when, tool, w, reason)
			}
		}
	}
}

// shQuote quotes text for the shell of the fake Claude Code, which on
// Windows is the sh.exe of Git for Windows. Paths get forward slashes.
func shQuote(text string) string {
	return `'` + strings.ReplaceAll(filepath.ToSlash(text), `'`, `'\''`) + `'`
}

// The Question of the Grill and Ana's answer.
const (
	grillQuestion = "Which customers fail at checkout today?"
	grillAnswer   = "EU customers whose bank asks for 3-D Secure."
)

// AC-1, AC-2, AC-3: Ana sets up Claude Code, grills her Intent with it and
// has it write plan.md; Ben approves; Ana starts the plan and Claude Code
// may edit only then.
func TestFromAnIntentThroughAGrillToAnApprovedPlanThatClaudeCodeImplements(t *testing.T) {
	ana, bare := startProject(t)

	// bfc setup claude, committed to main like any code (ADR 0010).
	ana.bfc("setup", "claude")
	settings, err := os.ReadFile(filepath.Join(ana.dir, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(settings), "bfc hook pre-tool-use") {
		t.Fatalf("ana: bfc setup claude wrote no hook: %v\n%s", err, settings)
	}
	if err := os.WriteFile(filepath.Join(ana.dir, "notes.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ana.git("add", ".claude", "notes.txt")
	ana.git("commit", "-q", "-m", "Set up Claude Code")
	ana.git("push", "-q", "origin", "main")

	// AC-2: on a branch without a Current plan, no edits.
	ana.blocked("without a Current plan", "bfc start <id>", "bfc prime")

	short := ana.newPlan("Payments via Stripe")
	ana.useEditor(`printf '\nCard payments fail for some EU customers. Move checkout to Stripe.\n' >> "$1"`)
	ana.bfc("edit", "--intent", short)

	// AC-1: the Grill. Claude Code reads the Intent and asks; Ana answers.
	qFile := filepath.Join(t.TempDir(), "question.json")
	ana.claude("bfc prime grill " + short + " >/dev/null && bfc ask " + short +
		" --to ana@example.com --ref intent.md --json -m " + shQuote(grillQuestion) + " > " + shQuote(qFile))
	var q struct {
		ID    string   `json:"id"`
		Agent string   `json:"agent"`
		To    []string `json:"to"`
	}
	data, err := os.ReadFile(qFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &q); err != nil || q.Agent != "claude-code" || len(q.To) != 1 || q.To[0] != "ana@example.com" {
		t.Fatalf("ana: the Grill's Question = %s (%v)", data, err)
	}
	ana.bfc("reply", q.ID, "-m", grillAnswer)
	grill := ana.bfc("prime", "grill", short)
	for _, want := range []string{grillQuestion, grillAnswer, "(answered)", "Reply", "by Ana"} {
		if !strings.Contains(grill, want) {
			t.Errorf("ana: bfc prime grill lacks %q:\n%s", want, grill)
		}
	}

	// AC-1: Claude Code writes plan.md from the Grill with bfc edit --stdin.
	planFile := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planFile, []byte(planMD), 0o600); err != nil {
		t.Fatal(err)
	}
	ana.claude("bfc prime plan " + short + " | grep -qF " + shQuote(grillAnswer) + " && bfc edit " + short + " --stdin < " + shQuote(planFile))
	var shown struct {
		State              string   `json:"state"`
		Plan               *string  `json:"plan"`
		AcceptanceCriteria []string `json:"acceptance_criteria"`
		OpenQuestions      []any    `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(ana.bfc("show", short, "--json")), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.State != "draft" || shown.Plan == nil || *shown.Plan != planMD || len(shown.AcceptanceCriteria) != 1 || len(shown.OpenQuestions) != 0 {
		t.Errorf("ana: after the write-plan turn, show --json = %+v", shown)
	}

	// AC-3: the Gates refuse to run inside Claude Code and write nothing.
	before := ana.git("rev-parse", plansRef)
	for _, gate := range [][]string{
		{"submit", short}, {"approve", short}, {"request-changes", short, "-m", "no"}, {"withdraw", short}, {"close", short},
	} {
		r := ana.agentBfc("", gate...)
		if r.err == nil || !strings.Contains(r.stderr, "for a person, not an agent") {
			t.Errorf("ana: bfc %s inside Claude Code: err = %v\n%s%s", gate[0], r.err, r.stdout, r.stderr)
		}
	}
	if after := ana.git("rev-parse", plansRef); after != before {
		t.Errorf("ana: a refused Gate changed the plans ref from %s to %s", before, after)
	}

	ana.bfc("submit", short)
	ana.bfc("push")

	// AC-2: before approval the Current plan does not allow edits.
	ana.bfc("use", short)
	ana.blocked("in review", short, "in-review", "approved")
	ana.bfc("use", "--clear")

	ben := clone(t, bare, "Ben")
	ben.bfc("bootstrap")
	ben.bfc("approve", short)
	ben.bfc("push")

	// AC-1: Ana starts the plan; Claude Code primes itself and may edit.
	ana.bfc("pull")
	if out := ana.bfc("start", short); !strings.Contains(out, "created and switched to "+short+"-payments-via-stripe") {
		t.Errorf("ana: bfc start printed:\n%s", out)
	}
	if branch := ana.git("branch", "--show-current"); branch != short+"-payments-via-stripe" {
		t.Errorf("ana: after bfc start the branch is %s", branch)
	}
	prime := ana.agentBfc("", "prime")
	if prime.err != nil {
		t.Fatalf("ana: bfc prime: %v\n%s", prime.err, prime.stderr)
	}
	for _, want := range []string{"Current plan: " + short + " Payments via Stripe", "State: approved", "AC-1: A card"} {
		if !strings.Contains(prime.stdout, want) {
			t.Errorf("ana: bfc prime lacks %q:\n%s", want, prime.stdout)
		}
	}
	for _, tool := range []string{"Edit", "Write"} {
		if reason := ana.hook(tool); reason != "" {
			t.Errorf("ana: on the approved plan the hook denied %s: %s", tool, reason)
		}
	}

	// AC-2: back on main there is no Current plan again.
	ana.git("checkout", "-q", "main")
	ana.blocked("back on main", "bfc start <id>")
	ana.sameRefAs(bare)
}
