package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
)

// exportFixture is a plan in progress with a Task handed off, a check with
// an Acknowledgement and a short Discussion: every section of the export.
func exportFixture() exportView {
	at := func(m int) time.Time { return time.Date(2026, 11, 20, 10, m, 0, 0, time.UTC) }
	id := func(s string) ulid.ULID { return ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C" + s) }
	p := index.Plan{
		ID: planID, ShortID: "bfc-5v7x", Title: "Payments via Stripe", Labels: []string{"payments"},
		LastActivity: at(30),
		Status: core.Status{
			State: core.StateInProgress, ShortID: "bfc-5v7x", Author: adaP, RequiredApprovals: 1, Approvers: []string{"ben@example.com"},
			Tasks: map[string]core.TaskStatus{"T-1": {State: core.TaskPaused, Claim: core.Claim{Holder: adaP}, Handoff: id("9H").String()}},
		},
		Intent: []byte("---\ntitle: Payments via Stripe\nlabels: [payments]\n---\n\n## Problem\n\nCards fail.\n"),
		Plan: []byte(strings.Replace(validPlan, "## Context", "# A stray title\n\n## Context", 1) +
			"\n```\n## inside a fence\n```\n"),
	}
	q := core.Message{ID: id("50"), Author: benP, At: at(5), To: []string{"ada@example.com"}, Ref: "plan.md#AC-1", Body: "Which cards?"}
	answer := core.Message{ID: id("51"), Author: adaP, At: at(6), ReplyTo: q.ID.String(), Body: "3-D Secure ones."}
	ack := core.Message{ID: id("52"), Author: adaP, At: at(20), Ref: "checks/" + id("40").String(), Body: "Agreed with Ben."}
	check := core.Check{
		ID: id("40"), Author: benP, At: at(15), Plan: planID, PR: 7, PlanBlob: "old",
		Criteria:   []core.CriterionResult{{ID: "AC-1", Status: core.NotMet}},
		OutOfScope: []string{"Touches the email worker."}, Summary: "AC-1 is not met.",
	}
	msgs := []core.Message{q, answer, ack}
	return exportView{
		Plan:     p,
		Messages: msgs,
		Checks:   core.PullRequestChecks([]core.Check{check}, "cur", msgs),
		Handoffs: []core.Handoff{{ID: id("9H"), Author: adaP, At: at(25), Task: "T-1", Note: "Half done.\n\n## Next\n\nThe retries."}},
		HandoffEvents: map[ulid.ULID]string{
			id("9H"): id("9J").String(),
		},
	}
}

// Issue #117 AC-1, AC-2, AC-3: one Markdown document with one # title, the
// documents without front matter and with their headings one level down,
// the Tasks, the Checks and the Discussion, stable for the same input.
func TestExportRendersThePlan(t *testing.T) {
	t.Parallel()
	out := renderExport(exportFixture())
	golden(t, out)
	if n := strings.Count("\n"+out, "\n# "); n != 1 {
		t.Errorf("%d top-level titles, want 1", n)
	}
	for _, unwanted := range []string{"title: Payments via Stripe", "\n## Problem", "\n# A stray title"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("export has %q", unwanted)
		}
	}
	if !strings.Contains(out, "\n## inside a fence\n") {
		t.Errorf("a heading inside a code fence was shifted")
	}
}

// Issue #117 AC-1, AC-4: bfc export of a closed plan prints it to stdout
// or writes the file of -o, and never writes to the Plans ref.
func TestExportCommand(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "ask", short, "--to", "ben@example.com", "-m", "EUR only?")
	mustBfc(t, dir, "close", short, "-m", "Not needed.")
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := mustBfc(t, dir, "export", short)
	for _, want := range []string{"# " + short + ": Payments\n", "- **State:** closed", "## Plan", "### Context", "## Discussion", "EUR only?", "Not needed."} {
		if !strings.Contains(out, want) {
			t.Errorf("export lacks %q:\n%s", want, out)
		}
	}
	file := filepath.Join(t.TempDir(), "plan.md")
	if printed := mustBfc(t, dir, "export", short, "-o", file); strings.Contains(printed, "## Plan") {
		t.Errorf("-o also printed the export:\n%s", printed)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != out {
		t.Errorf("the file of -o differs from stdout: %v", err)
	}
	if gitIn(t, dir, "rev-parse", plansRef) != before {
		t.Errorf("export wrote to the Plans ref")
	}
}

// Issue #117 AC-2: every kind of heading moves one level down, except in
// code fences and HTML comments.
func TestShiftHeadings(t *testing.T) {
	t.Parallel()
	in := strings.Join([]string{
		"Title", "=====", "",
		"Section", "-------", "",
		"  ## Indented", "",
		"<!--", "# in a comment", "-->", "",
		"~~~", "```", "# in a tilde fence", "~~~", "",
		"````", "```", "# in a long fence", "```", "````", "",
		"###### Six", "",
		"Not a heading", "",
		"- item", "",
		"---",
	}, "\n")
	want := strings.Join([]string{
		"## Title", "",
		"### Section", "",
		"  ### Indented", "",
		"<!--", "# in a comment", "-->", "",
		"~~~", "```", "# in a tilde fence", "~~~", "",
		"````", "```", "# in a long fence", "```", "````", "",
		"###### Six", "",
		"Not a heading", "",
		"- item", "",
		"---",
	}, "\n")
	if got := shiftHeadings(in); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
