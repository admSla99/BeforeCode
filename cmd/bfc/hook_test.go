package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// hookResult is what Claude Code gets from bfc hook pre-tool-use.
type hookResult struct {
	err    error  // the command's error: a non-zero exit
	stdout string // the decision as JSON, "" to allow
	stderr string
}

// denied returns the reason when the hook blocks the call.
func (r hookResult) denied(t *testing.T) (string, bool) {
	t.Helper()
	if r.stdout == "" {
		return "", false
	}
	var out struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, r.stdout)
	}
	h := out.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || h.PermissionDecisionReason == "" {
		t.Fatalf("hook output = %s", r.stdout)
	}
	return h.PermissionDecisionReason, true
}

// runHook runs bfc hook pre-tool-use in dir with the PreToolUse JSON of a
// call of tool with input, as Claude Code sends it.
func runHook(t *testing.T, dir, tool string, input map[string]any) hookResult {
	t.Helper()
	in, err := json.Marshal(map[string]any{
		"session_id":      "s1",
		"transcript_path": filepath.Join(dir, "t.jsonl"),
		"cwd":             dir,
		"permission_mode": "default",
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"tool_input":      input,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runHookRaw(t, dir, string(in))
}

func runHookRaw(t *testing.T, dir, stdin string) hookResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := newRootCmd("dev")
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"hook", "pre-tool-use"})
	err := cmd.ExecuteContext(inPlace(t, dir))
	return hookResult{err: err, stdout: stdout.String(), stderr: stderr.String()}
}

// AC-1: without a Current plan an edit is blocked with what to do.
func TestHookBlocksAnEditWithoutACurrentPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)

	r := runHook(t, dir, "Edit", map[string]any{"file_path": filepath.Join(dir, "main.go"), "old_string": "a", "new_string": "b"})

	if r.err != nil {
		t.Fatalf("hook failed: %v\n%s", r.err, r.stderr)
	}
	reason, ok := r.denied(t)
	if !ok || !strings.Contains(reason, "bfc start <id>") || !strings.Contains(reason, "bfc prime") {
		t.Errorf("reason = %q, denied = %t", reason, ok)
	}
}

// approvedPlan returns a repository whose Current plan (bfc use) is
// approved, with its Short ID.
func approvedPlan(t *testing.T) (dir, short string) {
	t.Helper()
	dir, short, _ = draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)
	as(t, dir, adaP)
	mustBfc(t, dir, "use", short)
	return dir, short
}

// AC-1: the edit tools are allowed only when the Current plan is approved.
func TestHookAllowsEditsOnlyWhenTheCurrentPlanIsApproved(t *testing.T) {
	t.Parallel()
	approved, _ := approvedPlan(t)
	draft, short, _ := draftWithPlan(t)
	mustBfc(t, draft, "use", short)

	edits := map[string]map[string]any{
		"Edit":         {"file_path": "main.go", "old_string": "a", "new_string": "b"},
		"Write":        {"file_path": "main.go", "content": "package main\n"},
		"MultiEdit":    {"file_path": "main.go", "edits": []any{}},
		"NotebookEdit": {"notebook_path": "a.ipynb", "new_source": "x"},
	}
	for tool, input := range edits {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			if r := runHook(t, approved, tool, abs(approved, input)); r.err != nil || r.stdout != "" {
				t.Errorf("approved: err = %v, stdout = %s", r.err, r.stdout)
			}
			r := runHook(t, draft, tool, abs(draft, input))
			reason, ok := r.denied(t)
			if r.err != nil || !ok || !strings.Contains(reason, short) || !strings.Contains(reason, "draft") ||
				!strings.Contains(reason, "approved") || !strings.Contains(reason, "bfc pull") || !strings.Contains(reason, "bfc prime") {
				t.Errorf("draft: err = %v, reason = %q", r.err, reason)
			}
		})
	}
}

// abs makes the paths of a tool input absolute in dir, as Claude Code sends
// them.
func abs(dir string, input map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range input {
		if p, ok := v.(string); ok && (k == "file_path" || k == "notebook_path") {
			v = filepath.Join(dir, p)
		}
		out[k] = v
	}
	return out
}

// AC-1: every other tool is allowed, Bash included, even where the hook
// would block an edit.
func TestHookAllowsOtherTools(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	for tool, input := range map[string]map[string]any{
		"Bash": {"command": "echo hi > main.go"},
		"Read": {"file_path": filepath.Join(dir, "main.go")},
		"Grep": {"pattern": "x"},
	} {
		if r := runHook(t, dir, tool, input); r.err != nil || r.stdout != "" {
			t.Errorf("%s: err = %v, stdout = %s", tool, r.err, r.stdout)
		}
	}
}

// AC-2: files under .beforecode/ and .claude/ of the repository, and files
// outside it, are edited without an approved plan. Paths are compared
// relative to the repository root, also when they are relative to the
// session's directory, reach the root through a symbolic link, or differ in
// case on Windows.
func TestHookAllowsEditsOfBfcAndClaudeFilesAndOutsideTheRepository(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	type pathCase struct {
		path  string
		allow bool
	}
	cases := []pathCase{
		{filepath.Join(dir, ".beforecode", "config.yaml"), true},
		{filepath.Join(dir, ".beforecode", "skills", "new", "SKILL.md"), true},
		{filepath.Join(dir, ".claude", "settings.json"), true},
		{filepath.Join(dir, "src", "..", ".claude", "x.md"), true},
		{filepath.Join(".claude", "settings.json"), true},
		{filepath.Join(t.TempDir(), "notes.md"), true},
		{filepath.Join(dir, "main.go"), false},
		{filepath.Join(dir, ".claude", "..", "main.go"), false},
		{filepath.Join(dir, ".beforecode-old", "x"), false},
		{filepath.Join(dir, "docs", ".claude", "x.md"), false},
		{filepath.Join(dir, ".claude"), false},
		{"main.go", false},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			pathCase{strings.ToUpper(filepath.Join(dir, ".claude", "x.md")), true},
			pathCase{strings.ToUpper(filepath.Join(dir, "main.go")), false})
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err == nil {
		cases = append(cases,
			pathCase{filepath.Join(link, ".claude", "skills", "beforecode", "SKILL.md"), true},
			pathCase{filepath.Join(link, "main.go"), false})
	} else {
		t.Logf("no symbolic links here, so no case reaches the root through one: %v", err)
	}
	for _, c := range cases {
		r := runHook(t, dir, "Write", map[string]any{"file_path": c.path, "content": "x"})
		if _, denied := r.denied(t); r.err != nil || denied == c.allow {
			t.Errorf("%s: err = %v, allowed = %t, want %t", c.path, r.err, !denied, c.allow)
		}
	}
}

// AC-3 and AC-5: when the hook cannot tell, it blocks an edit with the exact
// fix, and it never asks the remote.
func TestHookBlocksWithTheFixWhenItCannotTell(t *testing.T) {
	t.Parallel()
	noRef, _ := newClone(t)
	damaged := newLocalRepo(t)
	newPlan(t, damaged, "Payments")
	dropFromRef(t, damaged, "format-version")
	notARepo := t.TempDir()

	for name, c := range map[string]struct {
		dir   string
		stdin string
		want  []string
	}{
		"no plans ref":       {noRef, "", []string{"bfc bootstrap", "bfc init", "bfc doctor"}},
		"damaged plans ref":  {damaged, "", []string{"bfc doctor"}},
		"not a repository":   {notARepo, "", []string{"not inside a git working tree", "bfc doctor"}},
		"unreadable input":   {damaged, "{not json", []string{"cannot read the PreToolUse input"}},
		"no tool input path": {noRef, `{"tool_name":"Write","tool_input":{}}`, []string{"bfc bootstrap"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			trace := filepath.Join(t.TempDir(), "trace")
			setEnv(t, "GIT_TRACE", trace)
			var r hookResult
			if c.stdin == "" {
				r = runHook(t, c.dir, "Edit", map[string]any{"file_path": filepath.Join(c.dir, "main.go")})
			} else {
				r = runHookRaw(t, c.dir, c.stdin)
			}
			reason, ok := r.denied(t)
			if r.err != nil || !ok {
				t.Fatalf("err = %v, denied = %t", r.err, ok)
			}
			for _, w := range c.want {
				if !strings.Contains(reason, w) {
					t.Errorf("reason %q lacks %q", reason, w)
				}
			}
			data, _ := os.ReadFile(trace)
			for _, remote := range []string{"ls-remote", "fetch", "upload-pack"} {
				if strings.Contains(string(data), remote) {
					t.Errorf("the hook ran git %s", remote)
				}
			}
		})
	}
}

// AC-4: with claude.enforce: false the hook allows every edit; a config file
// it cannot read blocks with the fix.
func TestHookAllowsEverythingWhenEnforceIsOff(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	cfg := filepath.Join(dir, ".beforecode", "config.yaml")
	main := map[string]any{"file_path": filepath.Join(dir, "main.go"), "content": "x"}

	writeFile(t, cfg, "version: 3\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\nclaude:\n  enforce: false\n")
	if r := runHook(t, dir, "Write", main); r.err != nil || r.stdout != "" {
		t.Errorf("enforce off: err = %v, stdout = %s", r.err, r.stdout)
	}

	writeFile(t, cfg, "version: 3\nclaude:\n  enforce: maybe\n")
	r := runHook(t, dir, "Write", main)
	if reason, ok := r.denied(t); r.err != nil || !ok || !strings.Contains(reason, "config.yaml") || !strings.Contains(reason, "bfc doctor") {
		t.Errorf("bad config: err = %v, reason = %q", r.err, reason)
	}
}

// buildManyPlans makes repo, a repository with 500 plans whose Current plan
// (bfc use) is approved.
func buildManyPlans(dir string) error {
	if err := buildLocalRepo(dir); err != nil {
		return err
	}
	repo := filepath.Join(dir, "repo")
	// A branch with commits, as in a real project.
	if err := gitSteps([]string{repo, "commit", "-q", "--allow-empty", "-m", "start"}); err != nil {
		return err
	}
	out, err := bfcRun(repo, "new", "--no-edit", "Payments")
	if err != nil {
		return fmt.Errorf("bfc new: %w\n%s", err, out)
	}
	short := strings.Fields(out)[0]
	s, err := store.Open(repo)
	if err != nil {
		return err
	}
	ids, err := listPlanIDs(context.Background(), s)
	if err != nil {
		return err
	}
	if _, err := s.Write(context.Background(), "plan", map[string][]byte{core.PlanDir(ids[0]) + "/plan.md": []byte(validPlan)}); err != nil {
		return err
	}
	// In writes of 50 plans: one write passes all its paths to one git
	// command, and Windows limits the length of a command line.
	for batch := range 10 {
		files := map[string][]byte{}
		for i := batch * 50; i < min(batch*50+50, 499); i++ {
			id := ulid.Make()
			created := core.Event{ID: id, Type: core.Created, Plan: id, Author: adaP, At: time.Now(), ShortID: "bfc-" + strings.ToLower(id.String()[10:16])}
			files[core.PlanDir(id)+"/intent.md"] = fmt.Appendf(nil, "---\ntitle: Plan %d\n---\n", i)
			files[core.PlanDir(id)+"/events/"+id.String()+".yaml"] = core.EncodeEvent(created)
		}
		if _, err := s.Write(context.Background(), "many plans", files); err != nil {
			return err
		}
	}
	if err := bfcSteps([]string{repo, "submit", short}); err != nil {
		return err
	}
	if err := gitSteps([]string{repo, "config", "user.email", janaP.Email}); err != nil {
		return err
	}
	if err := bfcSteps([]string{repo, "approve", short}, []string{repo, "use", short}); err != nil {
		return err
	}
	return gitSteps([]string{repo, "config", "user.email", adaP.Email})
}

// editInput is the PreToolUse input of an Edit of main.go in dir.
func editInput(dir string) string {
	return fmt.Sprintf(`{"cwd":%q,"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":%q}}`, dir, filepath.Join(dir, "main.go"))
}

// AC-5: with 500 plans an edit is answered within 200 ms. Edits follow
// each other with nothing changed in between, so the hook answers from what
// it remembers. The first edit after the plans ref moved, as after a
// bfc pull, does the full check; its time is logged, as is the time of all
// runs under -race, which makes the Go part of bfc several times slower.
// Each run is a whole bfc invocation in this process; the real hook also
// starts the binary.
func TestHookAnswersWithin200msWith500Plans(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(copyFixture(t, "many-plans", buildManyPlans), "repo")
	_, other := newPlan(t, dir, "Another plan")
	in := editInput(dir)
	run := func() time.Duration {
		start := time.Now()
		r := runHookRaw(t, dir, in)
		d := time.Since(start)
		if r.err != nil || r.stdout != "" {
			t.Fatalf("the approved plan is blocked: %v %s", r.err, r.stdout)
		}
		return d
	}

	writeRef(t, dir, "edit", map[string][]byte{core.PlanDir(other) + "/intent.md": []byte("---\ntitle: Another plan\n---\n\nChanged.\n")})
	t.Logf("first edit after the plans ref moved: %v", run())
	var times []time.Duration
	for range 5 {
		times = append(times, run())
	}
	slices.Sort(times)
	t.Logf("next edits: %v", times)
	if raceEnabled {
		return
	}
	if median := times[len(times)/2]; median > 200*time.Millisecond {
		t.Errorf("median %v, want at most 200ms", median)
	}
}

func BenchmarkHookWith500Plans(b *testing.B) {
	dir := filepath.Join(b.TempDir(), "repo")
	if err := buildManyPlans(filepath.Dir(dir)); err != nil {
		b.Fatal(err)
	}
	in := editInput(dir)
	for b.Loop() {
		var stdout bytes.Buffer
		cmd := newRootCmd("dev")
		cmd.SetIn(strings.NewReader(in))
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"hook", "pre-tool-use"})
		if err := cmd.ExecuteContext(withPlace(context.Background(), place{dir: dir})); err != nil || stdout.Len() > 0 {
			b.Fatalf("%v %s", err, stdout.String())
		}
	}
}

// gitStarts returns how many git processes a GIT_TRACE file records.
func gitStarts(t *testing.T, trace string) int {
	t.Helper()
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "trace: built-in: git ")
}

// AC-5: the hook remembers that the Current plan is approved, so an edit
// after an allowed one starts one git process on a branch with commits,
// which keeps it fast where starting git is slow (Windows).
func TestHookRemembersAnApprovedPlan(t *testing.T) {
	t.Parallel()
	dir, _ := approvedPlan(t)
	commit(t, dir)
	edit := map[string]any{"file_path": filepath.Join(dir, "main.go"), "old_string": "a", "new_string": "b"}
	if r := runHook(t, dir, "Edit", edit); r.err != nil || r.stdout != "" {
		t.Fatalf("first edit: err = %v, stdout = %s", r.err, r.stdout)
	}

	trace := filepath.Join(t.TempDir(), "trace")
	setEnv(t, "GIT_TRACE", trace)
	if r := runHook(t, dir, "Edit", edit); r.err != nil || r.stdout != "" {
		t.Fatalf("second edit: err = %v, stdout = %s", r.err, r.stdout)
	}
	if n := gitStarts(t, trace); n != 1 {
		t.Errorf("the second edit started %d git processes, want 1", n)
	}
}

// AC-1: what the hook remembers holds only while the plans ref, the branch
// and the plan set with bfc use stay the same.
func TestHookChecksAgainWhenThePlanOrTheBranchChanges(t *testing.T) {
	t.Parallel()
	edit := func(dir string) map[string]any {
		return map[string]any{"file_path": filepath.Join(dir, "main.go"), "content": "x"}
	}
	// other is a draft made before the first edit, so that only the branch
	// or bfc use changes, not the plans ref.
	for name, change := range map[string]func(t *testing.T, dir, short, other string){
		"the plan is withdrawn": func(t *testing.T, dir, short, _ string) {
			mustBfc(t, dir, "withdraw", short)
		},
		"bfc use sets another plan": func(t *testing.T, dir, _, other string) {
			mustBfc(t, dir, "use", other)
		},
		"the branch names another plan": func(t *testing.T, dir, _, other string) {
			gitIn(t, dir, "switch", "-q", "-c", "feat/"+other)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, short := approvedPlan(t)
			other, _ := newPlan(t, dir, "Other")
			if r := runHook(t, dir, "Write", edit(dir)); r.err != nil || r.stdout != "" {
				t.Fatalf("approved: err = %v, stdout = %s", r.err, r.stdout)
			}
			change(t, dir, short, other)
			if _, denied := runHook(t, dir, "Write", edit(dir)).denied(t); !denied {
				t.Error("the edit is still allowed")
			}
		})
	}
}
