package tui

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

func init() {
	// Golden files show times in local time, as bfc list does; pin it.
	time.Local = time.UTC
}

// updating reports whether -update was given. teatest defines the flag.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// golden compares got with testdata/<test name>.golden, or rewrites the
// file with -update.
func golden(t *testing.T, got string) {
	t.Helper()
	path := filepath.Join("testdata", strings.ReplaceAll(t.Name(), "/", "_")+".golden")
	if updating() {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/tui -update)", err)
	}
	if got != string(want) {
		t.Errorf("screen differs from %s:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// fakePlans is the test adapter of Plans.
type fakePlans struct {
	plans   []PlanRow
	err     error
	created []string // titles passed to NewPlan
}

func (f *fakePlans) Load(context.Context) ([]PlanRow, error) { return f.plans, f.err }

func (f *fakePlans) NewPlan(title string) tea.Cmd {
	return func() tea.Msg {
		f.created = append(f.created, title)
		// Load returns the newest activity first.
		f.plans = append([]PlanRow{{ShortID: "bfc-n3w1", State: core.StateDraft, Title: title, Author: "ada@example.com", LastActivity: at(18, 0)}}, f.plans...)
		return PlanCreatedMsg{}
	}
}

// The list screen never calls the detail methods.
func (f *fakePlans) Detail(context.Context, string) (PlanDetail, error) {
	return PlanDetail{}, errors.New("not in the list tests")
}
func (f *fakePlans) Edit(string, Doc) tea.Cmd { return nil }
func (f *fakePlans) Review(context.Context, string, ReviewAction, string) error {
	return errors.New("not in the list tests")
}

func at(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, time.UTC) }

func team() *fakePlans {
	return &fakePlans{plans: []PlanRow{
		{"bfc-5v7x", core.StateInReview, "Payments via Stripe", "jana@example.com", at(17, 9)},
		{"bfc-k2m9", core.StateApproved, "Rate-limit the public API", "peter@example.com", at(17, 8)},
		{"bfc-q8r1", core.StateDraft, "Export plans to HTML", "ada@example.com", at(16, 21)},
		{"bfc-a7c3", core.StateInReview, "Move the index to SQLite FTS5", "ada@example.com", at(16, 17)},
		{"bfc-z0e4", core.StateDraft, "Dark mode for the TUI", "ben@example.com", at(15, 11)},
		{"bfc-m3n6", core.StateClosed, "Replace Cobra with urfave/cli", "ben@example.com", at(14, 9)},
		{"bfc-t5y2", core.StateApproved, "Retry pushes with backoff", "jana@example.com", at(13, 15)},
		{"bfc-0bad", core.StateInvalid, "(unreadable intent.md)", "", at(12, 10)},
	}}
}

// run starts the screen at w x h, waits until it shows want, sends the
// keys, quits and returns the final screen without styling.
func run(t *testing.T, plans Plans, w, h int, want string, keys ...string) string {
	t.Helper()
	return runUntil(t, plans, w, h, want, "", keys...)
}

// runUntil is run that, after the keys, also waits until the screen shows
// then, for results that arrive asynchronously.
func runUntil(t *testing.T, plans Plans, w, h int, want, then string, keys ...string) string {
	t.Helper()
	tm := teatest.NewTestModel(t, NewPlanList(plans), teatest.WithInitialTermSize(w, h))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), want) },
		teatest.WithDuration(3*time.Second))
	for _, k := range keys {
		tm.Send(press(k))
	}
	if then != "" {
		teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), then) },
			teatest.WithDuration(3*time.Second))
	}
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	m := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second))
	return ansi.Strip(m.View().Content)
}

// press turns a key name such as "down", "ctrl+c" or "x" into a key press.
func press(k string) tea.Msg {
	switch k {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

func TestPlanListGroupsOpenPlansByState(t *testing.T) {
	golden(t, run(t, team(), 80, 24, "IN-REVIEW"))
}

// AC-2: arrows and j/k move the selection; it never leaves the list.
func TestPlanListMovesTheSelection(t *testing.T) {
	golden(t, run(t, team(), 80, 24, "IN-REVIEW", "down", "j", "j", "k", "up", "down", "down"))
}

func TestPlanListSelectionStopsAtTheEnds(t *testing.T) {
	keys := []string{"up", "k"}
	for range 20 {
		keys = append(keys, "j")
	}
	golden(t, run(t, team(), 80, 24, "IN-REVIEW", keys...))
}

// AC-2: / searches in title, Short ID and author; esc clears the search.
func TestPlanListSearch(t *testing.T) {
	golden(t, run(t, team(), 80, 24, "IN-REVIEW", "/", "A", "d", "a", "@", "enter"))
}

func TestPlanListSearchWithoutMatches(t *testing.T) {
	golden(t, run(t, team(), 80, 24, "IN-REVIEW", "/", "z", "z", "z", "enter"))
}

func TestPlanListEscClearsTheSearch(t *testing.T) {
	out := run(t, team(), 80, 24, "IN-REVIEW", "/", "s", "t", "r", "esc")
	if !strings.Contains(out, "Rate-limit the public API") || !strings.Contains(out, "(/ to search)") {
		t.Errorf("search not cleared:\n%s", out)
	}
}

// AC-2: s cycles the state filter: open, in-review, approved, draft,
// closed, all.
func TestPlanListStateFilter(t *testing.T) {
	for n, name := range []string{"in-review", "approved", "draft", "closed", "all", "open"} {
		t.Run(name, func(t *testing.T) {
			var keys []string
			for range n + 1 {
				keys = append(keys, "s")
			}
			golden(t, run(t, team(), 80, 24, "IN-REVIEW", keys...))
		})
	}
}

// AC-4
func TestPlanListEmptyRepository(t *testing.T) {
	golden(t, run(t, &fakePlans{}, 80, 24, "No plans yet"))
}

func TestPlanListNotSetUp(t *testing.T) {
	golden(t, run(t, &fakePlans{err: &NotSetUpError{}}, 80, 24, "not set up"))
}

func TestPlanListNotSetUpWithAdvice(t *testing.T) {
	advice := "origin already has plans, but this clone does not: run bfc bootstrap to get them"
	golden(t, run(t, &fakePlans{err: &NotSetUpError{Advice: advice}}, 80, 24, "not set up"))
}

func TestPlanListLoadError(t *testing.T) {
	out := run(t, &fakePlans{err: errors.New("git: broken pipe")}, 80, 24, "broken pipe")
	if !strings.Contains(out, "git: broken pipe") {
		t.Errorf("error not shown:\n%s", out)
	}
}

// AC-3: n asks for a title, creates the plan and shows it.
func TestPlanListNewPlan(t *testing.T) {
	plans := team()
	golden(t, runUntil(t, plans, 80, 30, "IN-REVIEW", "Cache", "n", "C", "a", "c", "h", "e", "enter"))
	if len(plans.created) != 1 || plans.created[0] != "Cache" {
		t.Errorf("NewPlan called with %q", plans.created)
	}
}

func TestPlanListNewPlanPrompt(t *testing.T) {
	golden(t, run(t, team(), 80, 24, "IN-REVIEW", "n", "P", "l", "a", "n"))
}

func TestPlanListNewPlanCancelledOrEmpty(t *testing.T) {
	plans := team()
	run(t, plans, 80, 24, "IN-REVIEW", "n", "x", "esc", "n", " ", "enter")
	if len(plans.created) != 0 {
		t.Errorf("NewPlan called with %q", plans.created)
	}
}

func TestPlanListNewPlanFailureIsShown(t *testing.T) {
	plans := &failingNew{fakePlans: team()}
	out := runUntil(t, plans, 80, 24, "IN-REVIEW", "status 1", "n", "x", "enter")
	if !strings.Contains(out, "bfc new: editor exited with status 1") {
		t.Errorf("failure not shown:\n%s", out)
	}
}

type failingNew struct{ *fakePlans }

func (f *failingNew) NewPlan(string) tea.Cmd {
	return func() tea.Msg { return PlanCreatedMsg{Err: errors.New("editor exited with status 1")} }
}

// AC-5: a small or resized terminal scrolls the list to the selection and
// keeps the help line on the bottom row.
func TestPlanListScrollsToTheSelection(t *testing.T) {
	golden(t, run(t, team(), 80, 12, "IN-REVIEW", "j", "j", "j", "j", "j"))
}

func TestPlanListAdaptsToResize(t *testing.T) {
	tm := teatest.NewTestModel(t, NewPlanList(team()), teatest.WithInitialTermSize(80, 24))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), "IN-REVIEW") })
	tm.Send(tea.WindowSizeMsg{Width: 40, Height: 10})
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).View().Content)
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Errorf("%d lines, want 10", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
	}
	golden(t, out)
}

func TestPlanListFitsIn80x24(t *testing.T) {
	out := run(t, team(), 80, 24, "IN-REVIEW", "s", "s", "s", "s", "s")
	lines := strings.Split(out, "\n")
	if len(lines) != 24 {
		t.Errorf("%d lines, want 24", len(lines))
	}
	for _, l := range lines {
		if ansi.StringWidth(l) > 80 {
			t.Errorf("line wider than 80: %q", l)
		}
	}
}

// AC-2: q and ctrl+c quit.
func TestPlanListQuits(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			tm := teatest.NewTestModel(t, NewPlanList(team()))
			teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), "IN-REVIEW") })
			tm.Send(press(k))
			tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
		})
	}
}

// Ctrl+C quits even while a search or a title is being typed.
func TestPlanListCtrlCQuitsWhileTyping(t *testing.T) {
	for _, start := range []string{"/", "n"} {
		t.Run(start, func(t *testing.T) {
			tm := teatest.NewTestModel(t, NewPlanList(team()))
			teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), "IN-REVIEW") })
			tm.Send(press(start))
			tm.Send(press("x"))
			tm.Send(press("ctrl+c"))
			tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
		})
	}
}

// Esc clears a search that was already confirmed with enter.
func TestPlanListEscClearsAConfirmedSearch(t *testing.T) {
	out := run(t, team(), 80, 24, "IN-REVIEW", "/", "z", "z", "enter", "esc")
	if !strings.Contains(out, "Payments via Stripe") || !strings.Contains(out, "(/ to search)") {
		t.Errorf("search not cleared:\n%s", out)
	}
}

// Every line fits the width, also long errors and advice.
func TestPlanListCutsLongLines(t *testing.T) {
	long := strings.Repeat("very long remote message ", 8)
	for name, plans := range map[string]*fakePlans{
		"error":  {err: errors.New(long)},
		"advice": {err: &NotSetUpError{Advice: long}},
	} {
		t.Run(name, func(t *testing.T) {
			out := run(t, plans, 80, 24, "very long")
			for _, l := range strings.Split(out, "\n") {
				if ansi.StringWidth(l) > 80 {
					t.Errorf("line wider than 80: %q", l)
				}
			}
		})
	}
}
