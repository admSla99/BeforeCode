package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/tui"
)

// helloSession is the session of the recorded hello.jsonl.
const helloSession = "af62e7a3-81bf-4feb-8823-550215fc2f29"

var (
	fakeClaudeOnce sync.Once
	fakeClaudeBin  string
	fakeClaudeErr  error
)

// fakeClaude builds the fake Claude Code of the agent adapter (#59) once.
func fakeClaude(t *testing.T) string {
	t.Helper()
	fakeClaudeOnce.Do(func() {
		fakeClaudeBin = filepath.Join(fixtureRoot, "fakeclaude")
		if runtime.GOOS == "windows" {
			fakeClaudeBin += ".exe"
		}
		out, err := exec.Command("go", "build", "-o", fakeClaudeBin, "../../internal/agent/claude/testdata/fakeclaude").CombinedOutput()
		if err != nil {
			fakeClaudeErr = err
			fakeClaudeBin = string(out)
		}
	})
	if fakeClaudeErr != nil {
		t.Fatalf("building fakeclaude: %v\n%s", fakeClaudeErr, fakeClaudeBin)
	}
	return fakeClaudeBin
}

func claudeFixture(name string) string {
	p, _ := filepath.Abs(filepath.Join("..", "..", "internal", "agent", "claude", "testdata", name))
	return p
}

// grillRepo returns a repository with a draft plan, set up to run the fake
// Claude Code that replays hello.jsonl and records its arguments.
func grillRepo(t *testing.T) (dir, short string, id ulid.ULID, argsFile string) {
	t.Helper()
	dir = newLocalRepo(t)
	short, id = newPlan(t, dir, "Payments")
	writeFile(t, filepath.Join(dir, ".beforecode", "config.yaml"),
		"version: 3\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\nclaude:\n  command: '"+fakeClaude(t)+"'\n")
	argsFile = filepath.Join(t.TempDir(), "args")
	setEnv(t, "FAKECLAUDE_ARGS", argsFile)
	setEnv(t, "FAKECLAUDE_FIXTURE", claudeFixture("hello.jsonl"))
	return dir, short, id, argsFile
}

// grillIn returns the Grill adapter in dir, with the variables set so far.
func grillIn(t *testing.T, dir string) *tuiGrill {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(inPlace(t, dir))
	return &tuiGrill{cmd: cmd}
}

// runTurn runs a turn to its end and returns the text and the error.
func runTurn(t *testing.T, g *tuiGrill, short string, turn tui.GrillTurn) (string, error) {
	t.Helper()
	ch, err := g.Turn(context.Background(), short, turn)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var turnErr error
	for ev := range ch {
		text.WriteString(ev.Text)
		if ev.Err != nil {
			turnErr = ev.Err
		}
	}
	return text.String(), turnErr
}

// claudeArgs returns the options the fake Claude Code got last, and the
// prompt after "--" (the recording joins arguments with newlines, so a
// prompt of several lines comes back whole only this way).
func claudeArgs(t *testing.T, file string) (opts []string, prompt string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(readFile(t, file)), "\n")[1:]
	i := slices.Index(lines, "--")
	if i < 0 {
		t.Fatalf("no -- in %q", lines)
	}
	return lines[:i], strings.Join(lines[i+1:], "\n")
}

func sessionFile(t *testing.T, dir string, id ulid.ULID) string {
	t.Helper()
	return filepath.Join(gitIn(t, dir, "rev-parse", "--absolute-git-dir"), "beforecode", "grill", id.String()+".session")
}

// AC-3, AC-5: the first turn starts a new session prompted with bfc prime
// grill, allows only bfc ask (and reading), streams the text and keeps the
// session; the next turn resumes it with the author's answer.
func TestGrillTurnStartsAndResumesTheSession(t *testing.T) {
	t.Parallel()
	dir, short, id, argsFile := grillRepo(t)

	g := grillIn(t, dir)
	text, err := runTurn(t, g, short, tui.GrillTurn{Kind: tui.TurnAsk})
	if err != nil || text != "hi" {
		t.Fatalf("turn: %q, %v", text, err)
	}
	args, prompt := claudeArgs(t, argsFile)
	if slices.Contains(args, "--resume") || !strings.Contains(prompt, "bfc prime grill "+short) {
		t.Errorf("first turn: %q, %q", args, prompt)
	}
	for _, want := range []string{"Bash(bfc ask:*)", "Bash(bfc prime:*)"} {
		if !slices.Contains(args, want) {
			t.Errorf("allowed tools lack %s: %q", want, args)
		}
	}
	if slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, "Bash(bfc reply") || strings.HasPrefix(a, "Bash(bfc edit")
	}) {
		t.Errorf("a Grill turn may only ask: %q", args)
	}
	if got := strings.TrimSpace(readFile(t, sessionFile(t, dir, id))); got != helloSession {
		t.Errorf("session file = %q", got)
	}

	if _, err := runTurn(t, g, short, tui.GrillTurn{Kind: tui.TurnAsk, Answer: "Only 3-D Secure cards."}); err != nil {
		t.Fatal(err)
	}
	args, prompt = claudeArgs(t, argsFile)
	if i := slices.Index(args, "--resume"); i < 0 || args[i+1] != helloSession || !strings.Contains(prompt, "Only 3-D Secure cards.") {
		t.Errorf("second turn: %q, %q", args, prompt)
	}
}

// #92: when nothing is left to ask, the agent records no Question, which
// would stay open and block approval; the author finishes in the TUI.
func TestGrillTurnRecordsNoQuestionWhenNothingIsLeft(t *testing.T) {
	t.Parallel()
	dir, short, _, argsFile := grillRepo(t)
	g := grillIn(t, dir)
	for _, turn := range []tui.GrillTurn{{Kind: tui.TurnAsk}, {Kind: tui.TurnAsk, Answer: "Only 3-D Secure cards."}} {
		if _, err := runTurn(t, g, short, turn); err != nil {
			t.Fatal(err)
		}
		_, prompt := claudeArgs(t, argsFile)
		if !strings.Contains(prompt, "When nothing is left to ask, record no Question") || strings.Contains(prompt, "wants to finish") {
			t.Errorf("prompt = %q", prompt)
		}
	}
}

// AC-5: when --resume fails, the turn starts a new session from bfc prime
// grill, so nothing is lost.
func TestGrillTurnStartsAgainWhenTheSessionIsLost(t *testing.T) {
	t.Parallel()
	dir, short, id, argsFile := grillRepo(t)
	writeFile(t, sessionFile(t, dir, id), "00000000-0000-4000-8000-000000000000\n")
	setEnv(t, "FAKECLAUDE_RESUME_FIXTURE", claudeFixture("badresume.jsonl"))
	setEnv(t, "FAKECLAUDE_RESUME_STDERR", readFile(t, claudeFixture("badresume.stderr")))
	setEnv(t, "FAKECLAUDE_RESUME_EXIT", "1")

	text, err := runTurn(t, grillIn(t, dir), short, tui.GrillTurn{Kind: tui.TurnAsk, Answer: "Only 3-D Secure cards."})
	if err != nil || text != "hi" {
		t.Fatalf("turn: %q, %v", text, err)
	}
	args, prompt := claudeArgs(t, argsFile)
	if slices.Contains(args, "--resume") || !strings.Contains(prompt, "bfc prime grill "+short) {
		t.Errorf("the new session: %q, %q", args, prompt)
	}
	if got := strings.TrimSpace(readFile(t, sessionFile(t, dir, id))); got != helloSession {
		t.Errorf("session file = %q, want the new session", got)
	}
}

// AC-4: a failing claude ends the turn with its error.
func TestGrillTurnReportsAFailure(t *testing.T) {
	t.Parallel()
	dir, short, _, _ := grillRepo(t)
	setEnv(t, "FAKECLAUDE_FIXTURE", "")
	setEnv(t, "FAKECLAUDE_STDERR", "rate limited")
	setEnv(t, "FAKECLAUDE_EXIT", "1")
	if _, err := runTurn(t, grillIn(t, dir), short, tui.GrillTurn{Kind: tui.TurnAsk}); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("err = %v, want the claude error", err)
	}
}

// bfcOnce builds this bfc once, for the fake Claude Code to run.
var (
	bfcOnce sync.Once
	bfcBin  string
	bfcErr  error
)

func bfcBinary(t *testing.T) string {
	t.Helper()
	bfcOnce.Do(func() {
		bfcBin = filepath.Join(fixtureRoot, "bfc")
		if runtime.GOOS == "windows" {
			bfcBin += ".exe"
		}
		out, err := exec.Command("go", "build", "-o", bfcBin, ".").CombinedOutput()
		if err != nil {
			bfcErr, bfcBin = err, string(out)
		}
	})
	if bfcErr != nil {
		t.Fatalf("building bfc: %v\n%s", bfcErr, bfcBin)
	}
	return bfcBin
}

// runTurnEvents runs a turn and returns all its events.
func runTurnEvents(t *testing.T, g *tuiGrill, short string, turn tui.GrillTurn) []tui.GrillEvent {
	t.Helper()
	ch, err := g.Turn(context.Background(), short, turn)
	if err != nil {
		t.Fatal(err)
	}
	var evs []tui.GrillEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func planSaved(evs []tui.GrillEvent) bool {
	return slices.ContainsFunc(evs, func(e tui.GrillEvent) bool { return e.PlanSaved })
}

// AC-7: the write-plan turn is prompted with bfc prime plan, may use
// bfc edit, and uses --force only when the author agreed. Only a plan.md
// that was really stored ends the Grill: it reports PlanSaved and removes
// the session file.
func TestGrillTurnWritesThePlan(t *testing.T) {
	t.Parallel()
	dir, short, id, argsFile := grillRepo(t)
	g := grillIn(t, dir)
	if _, err := runTurn(t, g, short, tui.GrillTurn{Kind: tui.TurnAsk}); err != nil {
		t.Fatal(err)
	}
	evs := runTurnEvents(t, g, short, tui.GrillTurn{Kind: tui.TurnWritePlan})
	args, prompt := claudeArgs(t, argsFile)
	if !slices.Contains(args, "Bash(bfc edit:*)") || !strings.Contains(prompt, "bfc prime plan "+short) || strings.Contains(prompt, "--force") {
		t.Errorf("write-plan turn: %q, %q", args, prompt)
	}
	if planSaved(evs) {
		t.Error("PlanSaved without a stored plan.md")
	}
	if _, err := os.Stat(sessionFile(t, dir, id)); err != nil {
		t.Errorf("the session file must stay while plan.md is not written: %v", err)
	}

	// The agent stores plan.md with bfc edit --stdin.
	planFile := filepath.Join(t.TempDir(), "plan.md")
	writeFile(t, planFile, validPlan)
	setEnv(t, "FAKECLAUDE_EXEC", "'"+bfcBinary(t)+"' edit "+short+" --stdin < '"+planFile+"'")
	evs = runTurnEvents(t, grillIn(t, dir), short, tui.GrillTurn{Kind: tui.TurnWritePlan, Force: true})
	if !planSaved(evs) {
		t.Errorf("no PlanSaved after bfc edit stored plan.md: %+v", evs)
	}
	if _, prompt := claudeArgs(t, argsFile); !strings.Contains(prompt, "--force") {
		t.Errorf("with the author's yes the prompt allows --force: %q", prompt)
	}
	if _, err := os.Stat(sessionFile(t, dir, id)); !os.IsNotExist(err) {
		t.Errorf("the session file stays after plan.md was written: %v", err)
	}
}

// AC-5: a summary in a new session reads the Grill first.
func TestGrillSummaryInANewSessionReadsTheGrill(t *testing.T) {
	t.Parallel()
	dir, short, _, argsFile := grillRepo(t)
	if _, err := runTurn(t, grillIn(t, dir), short, tui.GrillTurn{Kind: tui.TurnSummary}); err != nil {
		t.Fatal(err)
	}
	if _, prompt := claudeArgs(t, argsFile); !strings.Contains(prompt, "bfc prime grill "+short) {
		t.Errorf("summary prompt = %q", prompt)
	}
}

// AC-3: the snapshot holds the Grill's Questions from the AI to the author
// with their answers, other Messages apart, and the answer the TUI stores
// is the author's own words (no agent), so it answers the Question.
func TestGrillSnapshotAndAnswer(t *testing.T) {
	t.Parallel()
	dir, short, id, _ := grillRepo(t)
	setEnv(t, agentEnv, "1")
	q1 := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--ref", "intent.md", "-m", "Which cards fail?"))
	mustBfc(t, dir, "reply", q1, "--verbatim", "-m", "Only 3-D Secure cards.")
	q2 := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "How do we know it worked?"))
	setEnv(t, agentEnv, "")
	as(t, dir, benP)
	mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Refunds too?")
	mustBfc(t, dir, "reply", q1, "-m", "Also Maestro.")
	as(t, dir, adaP)

	g := grillIn(t, dir)
	snap, err := g.Snapshot(context.Background(), short)
	if err != nil {
		t.Fatal(err)
	}
	want := []tui.GrillQuestion{
		{ID: q1, Text: "Which cards fail?", Answer: "Only 3-D Secure cards."},
		{ID: q2, Text: "How do we know it worked?", Open: true},
	}
	if !slices.Equal(snap.Questions, want) {
		t.Errorf("questions = %+v, want %+v", snap.Questions, want)
	}
	if want := []tui.GrillMessage{{Who: "Ben Bitdiddle", Text: "Refunds too?"}, {Who: "Ben Bitdiddle", Text: "Also Maestro."}}; !slices.Equal(snap.Others, want) {
		t.Errorf("others = %+v, want %+v", snap.Others, want)
	}
	if snap.Author != "Ada Lovelace" || snap.AuthorEmail != "ada@example.com" || snap.PlanWritten {
		t.Errorf("snapshot = %+v", snap)
	}

	if err := g.Answer(context.Background(), short, q2, "A test card turns the order paid."); err != nil {
		t.Fatal(err)
	}
	snap, _ = g.Snapshot(context.Background(), short)
	if q := snap.Questions[1]; q.Open || q.Answer != "A test card turns the order paid." {
		t.Errorf("after the answer: %+v", q)
	}

	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	if snap, _ = g.Snapshot(context.Background(), short); !snap.PlanWritten {
		t.Error("a written plan.md is not reported")
	}
}

// The commands the agent runs reach the screen as its current activity.
func TestGrillTurnReportsTheAgentsTools(t *testing.T) {
	t.Parallel()
	dir, short, _, _ := grillRepo(t)
	setEnv(t, "FAKECLAUDE_FIXTURE", claudeFixture("tool.jsonl"))
	ch, err := grillIn(t, dir).Turn(context.Background(), short, tui.GrillTurn{Kind: tui.TurnAsk})
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for ev := range ch {
		if ev.Tool != "" {
			tools = append(tools, ev.Tool)
		}
	}
	if !slices.Equal(tools, []string{"Read"}) {
		t.Errorf("tools = %q, want the recorded Read call", tools)
	}
}
