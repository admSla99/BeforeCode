package claude_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/admSla99/BeforeCode/internal/agent"
	"github.com/admSla99/BeforeCode/internal/agent/claude"
)

// fakeClaude is the path of the fake Claude Code built by TestMain.
var fakeClaude string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fakeclaude")
	if err != nil {
		panic(err)
	}
	fakeClaude = filepath.Join(dir, "claude")
	if runtime.GOOS == "windows" {
		fakeClaude += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fakeClaude, "./testdata/fakeclaude").CombinedOutput(); err != nil {
		panic("building fakeclaude: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fixture returns the absolute path of a recorded stream-json file.
func fixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// collect runs a and returns every Event until the channel closes.
func collect(t *testing.T, ctx context.Context, a agent.Agent, task agent.Task) []agent.Event {
	t.Helper()
	events, err := a.Run(ctx, task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []agent.Event
	timeout := time.After(30 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return got
			}
			got = append(got, e)
		case <-timeout:
			t.Fatalf("the event channel did not close; got %+v", got)
		}
	}
}

func TestARecordedTurnBecomesTextAndAResultWithTheSession(t *testing.T) {
	t.Parallel()
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{"FAKECLAUDE_FIXTURE=" + fixture(t, "hello.jsonl")}})
	got := collect(t, t.Context(), a, agent.Task{Prompt: "Reply with exactly: hi", Dir: t.TempDir()})
	const session = "af62e7a3-81bf-4feb-8823-550215fc2f29"
	want := []agent.Event{
		{Kind: agent.Text, Text: "hi", Session: session},
		{Kind: agent.Result, Text: "hi", Session: session},
	}
	assertEvents(t, got, want)
}

func assertEvents(t *testing.T, got, want []agent.Event) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d:\n got %+v\nwant %+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.Text != w.Text || g.Session != w.Session || g.Tool != w.Tool || string(g.Input) != string(w.Input) {
			t.Errorf("event %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestAResumedTurnPassesTheSessionToolsAndDirectoryAndReportsTheToolUse(t *testing.T) {
	t.Parallel()
	const session = "af62e7a3-81bf-4feb-8823-550215fc2f29"
	record := filepath.Join(t.TempDir(), "args")
	dir := t.TempDir()
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{
		"FAKECLAUDE_FIXTURE=" + fixture(t, "tool.jsonl"),
		"FAKECLAUDE_ARGS=" + record,
	}})
	got := collect(t, t.Context(), a, agent.Task{
		Prompt:       "Read notes.txt with the Read tool and tell me its first word.",
		Session:      session,
		AllowedTools: []string{"Read", "Bash(bfc show:*)"},
		Dir:          dir,
	})
	answer := "The first word of `notes.txt` is **hello**."
	assertEvents(t, got, []agent.Event{
		{Kind: agent.ToolUse, Tool: "Read", Input: []byte(`{"file_path":"/work/notes.txt"}`), Session: session},
		{Kind: agent.Text, Text: answer, Session: session},
		{Kind: agent.Result, Text: answer, Session: session},
	})
	if got[0].ToolID != "toolu_011hiLm6ThKFG499rmAjZnAt" {
		t.Errorf("tool use id = %q", got[0].ToolID)
	}

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	wantDir, _ := filepath.EvalSymlinks(dir)
	want := "dir=" + wantDir + "\n" +
		"-p\nRead notes.txt with the Read tool and tell me its first word.\n" +
		"--output-format\nstream-json\n--verbose\n" +
		"--resume\n" + session + "\n" +
		"--allowedTools\nRead\nBash(bfc show:*)\n"
	if gotArgs := string(data); gotArgs != want {
		t.Errorf("claude was started with\n%s\nwant\n%s", gotArgs, want)
	}
}

func TestAFailedRunEndsWithOneErrorNamingTheCause(t *testing.T) {
	t.Parallel()
	stderr, err := os.ReadFile(fixture(t, "badresume.stderr"))
	if err != nil {
		t.Fatal(err)
	}
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{
		"FAKECLAUDE_FIXTURE=" + fixture(t, "badresume.jsonl"),
		"FAKECLAUDE_STDERR=" + string(stderr),
		"FAKECLAUDE_EXIT=1",
	}})
	got := collect(t, t.Context(), a, agent.Task{Prompt: "hi", Session: "00000000-0000-4000-8000-000000000000", Dir: t.TempDir()})
	assertEvents(t, got, []agent.Event{{
		Kind:    agent.Error,
		Text:    "No conversation found with session ID: 00000000-0000-4000-8000-000000000000",
		Session: "00000000-0000-4000-8000-000000000000",
	}})
	if got[0].Err == nil {
		t.Error("the Error Event has no Err")
	}
}

func TestAProcessThatExitsNonZeroWithoutAResultBecomesAnErrorWithItsStderr(t *testing.T) {
	t.Parallel()
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{
		"FAKECLAUDE_STDERR=Invalid API key · Please run /login\n",
		"FAKECLAUDE_EXIT=1",
	}})
	got := collect(t, t.Context(), a, agent.Task{Prompt: "hi", Dir: t.TempDir()})
	assertEvents(t, got, []agent.Event{{Kind: agent.Error, Text: "claude exited with status 1: Invalid API key · Please run /login"}})
}

func TestALineThatIsNotJSONBecomesAnError(t *testing.T) {
	t.Parallel()
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(bad, []byte("Welcome to Claude Code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{"FAKECLAUDE_FIXTURE=" + bad}})
	got := collect(t, t.Context(), a, agent.Task{Prompt: "hi", Dir: t.TempDir()})
	assertEvents(t, got, []agent.Event{{Kind: agent.Error, Text: `claude printed a line that is not JSON: "Welcome to Claude Code"`}})
}

func TestCancellingTheContextStopsTheProcess(t *testing.T) {
	t.Parallel()
	a := claude.New(claude.Options{Command: fakeClaude, Env: []string{
		"FAKECLAUDE_FIXTURE=" + fixture(t, "hello.jsonl"),
		"FAKECLAUDE_HANG=1",
	}})
	ctx, cancel := context.WithCancel(t.Context())
	events, err := a.Run(ctx, agent.Task{Prompt: "hi", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var got []agent.Event
	for e := range events {
		got = append(got, e)
		if e.Kind == agent.Result {
			cancel() // the fake now sleeps for an hour
		}
	}
	cancel()
	last := got[len(got)-1]
	if last.Kind != agent.Error || !errors.Is(last.Err, context.Canceled) {
		t.Errorf("last event = %+v, want an Error caused by context.Canceled", last)
	}
}

func TestACommandThatDoesNotExistFailsToStart(t *testing.T) {
	t.Parallel()
	a := claude.New(claude.Options{Command: filepath.Join(t.TempDir(), "no-claude")})
	if _, err := a.Run(t.Context(), agent.Task{Prompt: "hi", Dir: t.TempDir()}); err == nil {
		t.Error("Run succeeded")
	}
}
