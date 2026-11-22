package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/admSla99/BeforeCode/internal/core"
)

// inProgressTeam is the team with bfc-5v7x in progress: three Tasks, one
// completed, one claimed by the current user, one Paused.
func inProgressTeam() *fakeTeam {
	f := newFakeTeam()
	d := f.details["bfc-5v7x"]
	d.State = core.StateInProgress
	d.RequiredApprovals, d.Approvers = 0, nil
	d.Actions = []ReviewAction{ActionClose} // what anyone may do with an in-progress plan
	d.Tasks = []TaskRow{
		{ID: "T-1", Text: "Add the Stripe client.", State: core.TaskCompleted, Holder: "Jana Nováková", Latest: "Client in place, tested.",
			Handoffs: []HandoffRow{{ID: "01JD3K8Q2W5V7X9Y0Z1A2B3C9A", By: "Jana Nováková", Done: true, Note: "Client in place, tested."}}},
		{ID: "T-2", Text: "Wire the feature flag.", State: core.TaskClaimed, Holder: "Ada Lovelace", Mine: true},
		{ID: "T-3", Text: "Remove the old provider\nonce EU traffic is moved.", State: core.TaskPaused, Holder: "Peter", Latest: "Half done.",
			Handoffs: []HandoffRow{{ID: "01JD3K8Q2W5V7X9Y0Z1A2B3C9B", By: "Peter (through claude-code)", Note: "Half done.\nNext: the EU switch."}}},
	}
	f.details["bfc-5v7x"] = d
	return f
}

// Issue #112 AC-2, AC-3: the tasks tab lists each Task with its state,
// holder and the first line of its latest Handoff, marks my own Claim, and
// the status bar counts the completed Tasks.
func TestAppTasksTab(t *testing.T) {
	tm := startAppWith(t, inProgressTeam(), AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab", "tab")
	waitFor(t, tm, "T-3")
	golden(t, screen(t, tm))
}

// Issue #112 AC-2: Enter shows the selected Task's Handoffs; esc returns to
// the list of Tasks.
func TestAppTasksTabShowsHandoffs(t *testing.T) {
	tm := startAppWith(t, inProgressTeam(), AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab", "tab")
	waitFor(t, tm, "T-3")
	send(tm, "down", "down", "enter")
	waitFor(t, tm, "Next: the EU switch.")
	send(tm, "esc")
	waitFor(t, tm, "T-2")
	out := screen(t, tm)
	if strings.Contains(out, "Next: the EU switch.") || !strings.Contains(out, "T-1") {
		t.Errorf("esc did not return to the Tasks:\n%s", out)
	}
}

// A plan without Tasks says so in the tab.
func TestAppTasksTabWithoutTasks(t *testing.T) {
	tm := startAppWith(t, newFakeTeam(), AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab", "tab")
	waitFor(t, tm, "No Tasks")
	if out := screen(t, tm); !strings.Contains(out, "## Tasks") {
		t.Errorf("no hint where Tasks come from:\n%s", out)
	}
}

// Issue #112 AC-2: the Handoffs of the selected Task, in the viewport.
func TestAppTasksTabHandoffsView(t *testing.T) {
	tm := startAppWith(t, inProgressTeam(), AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab", "tab")
	waitFor(t, tm, "T-3")
	send(tm, "down", "down", "enter")
	waitFor(t, tm, "Next: the EU switch.")
	golden(t, screen(t, tm))
}

// Issue #112 AC-4: with more Tasks than lines, the selection stays in view.
func TestAppTasksTabScrollsToTheSelection(t *testing.T) {
	f := inProgressTeam()
	d := f.details["bfc-5v7x"]
	for i := 4; i <= 40; i++ {
		d.Tasks = append(d.Tasks, TaskRow{ID: fmt.Sprintf("T-%d", i), Text: "More work.", State: core.TaskOpen})
	}
	f.details["bfc-5v7x"] = d
	tm := startAppWith(t, f, AppOptions{Interval: time.Hour, Local: true})
	send(tm, "tab", "tab")
	waitFor(t, tm, "T-3")
	for range 39 {
		send(tm, "down")
	}
	waitFor(t, tm, "T-40")
	if out := screen(t, tm); !strings.Contains(out, "▶ T-40") {
		t.Errorf("the selected Task is not on the screen:\n%s", out)
	}
}
