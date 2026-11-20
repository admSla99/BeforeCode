package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// agent and stores the answers, has its own tests in internal/tui and
// cmd/bfc; here the person's answer goes through bfc reply, which shares
// its code.

// fakeClaudeBin builds the fake Claude Code once per test binary.
func fakeClaudeBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(filepath.Dir(bfcBin), "claude")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if _, err := os.Stat(bin); err == nil {
		return bin
	}
	build := exec.Command("go", "build", "-o", bin, "github.com/admSla99/BeforeCode/internal/agent/claude/testdata/fakeclaude")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build fakeclaude: %v\n%s", err, out)
	}
	return bin
}

// result is what one run of a program printed.
type result struct {
	stdout, stderr string
	err            error
}

// run runs a program in the developer's clone with the extra variables and
// stdin, with the directory of bfc first on PATH, as for an installed bfc.
func (d *dev) run(stdin string, env []string, name string, args ...string) result {
	d.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = d.dir
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(bfcBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return result{stdout.String(), stderr.String(), err}
}

// claude runs the fake Claude Code in the developer's clone. Its tool is
// the shell command script, run with CLAUDECODE=1 like every command Claude
// Code runs; the output is the recorded hello.jsonl.
func (d *dev) claude(script string) {
	d.t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "internal", "agent", "claude", "testdata", "hello.jsonl"))
	if err != nil {
		d.t.Fatal(err)
	}
	r := d.run("", []string{"CLAUDECODE=1", "FAKECLAUDE_EXEC=" + script, "FAKECLAUDE_FIXTURE=" + fixture}, fakeClaudeBin(d.t), "-p", "--output-format", "stream-json", "--verbose")
	if r.err != nil {
		d.t.Fatalf("%s: claude ran %q: %v\n%s%s", d.name, script, r.err, r.stdout, r.stderr)
	}
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
	in["cwd"] = d.dir
	input := in["tool_input"].(map[string]any)
	input["file_path"] = filepath.Join(d.dir, filepath.FromSlash(strings.TrimPrefix(input["file_path"].(string), "/work/")))
	stdin, err := json.Marshal(in)
	if err != nil {
		d.t.Fatal(err)
	}
	r := d.run(string(stdin), []string{"CLAUDECODE=1"}, bfcBin, "hook", "pre-tool-use")
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

// sh quotes a path for the fake's shell; bfc starts it through sh, which on
// Windows is the sh.exe of Git for Windows.
func sh(path string) string {
	return `'` + filepath.ToSlash(path) + `'`
}

// The Questions and answers of the Grill.
const (
	question = "Which customers fail at checkout today?"
	answer   = "EU customers whose bank asks for 3-D Secure."
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
		" --to ana@example.com --ref intent.md --json -m '" + question + "' > " + sh(qFile))
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
	ana.bfc("reply", q.ID, "-m", answer)
	grill := ana.bfc("prime", "grill", short)
	for _, want := range []string{question, answer, "(answered)", "Reply", "by Ana"} {
		if !strings.Contains(grill, want) {
			t.Errorf("ana: bfc prime grill lacks %q:\n%s", want, grill)
		}
	}

	// AC-1: Claude Code writes plan.md from the Grill with bfc edit --stdin.
	planFile := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(planFile, []byte(planMD), 0o600); err != nil {
		t.Fatal(err)
	}
	ana.claude("bfc prime plan " + short + " | grep -q '" + answer + "' && bfc edit " + short + " --stdin < " + sh(planFile))
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
		r := ana.run("", []string{"CLAUDECODE=1"}, bfcBin, gate...)
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
	prime := ana.run("", []string{"CLAUDECODE=1"}, bfcBin, "prime")
	for _, want := range []string{"Current plan: " + short + " Payments via Stripe", "State: approved", "AC-1: A card"} {
		if prime.err != nil || !strings.Contains(prime.stdout, want) {
			t.Errorf("ana: bfc prime lacks %q (%v):\n%s%s", want, prime.err, prime.stdout, prime.stderr)
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
