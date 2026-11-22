package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

const goodPlan = `## Context

Cards fail.

## approach

Use Stripe.

## Affected areas

payments/

## Out of scope

Refunds.

## Tasks

- T-1: Add the client.
- T-2: Handle the webhook idempotently,
  keyed by the event ID.

## Acceptance criteria

<!--
- AC-9: an example inside a comment is ignored
-->

- AC-1: With an invalid card the API returns 402.
- AC-3: A successful payment sends exactly one email,
  even when the webhook is delivered twice.
`

func messages(problems []core.Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, p.Message)
	}
	return strings.Join(out, "\n")
}

func TestValidatePlanAcceptsAPlanInTheDataFormat(t *testing.T) {
	if problems := core.ValidatePlan([]byte(goodPlan)); len(problems) != 0 {
		t.Errorf("problems in a valid plan:\n%s", messages(problems))
	}
}

func TestValidatePlanReportsEachProblem(t *testing.T) {
	cases := map[string]struct {
		plan string
		want []string
	}{
		"missing sections": {
			"## Context\n\n## Acceptance criteria\n\n- AC-1: x\n",
			[]string{`missing section "## Approach"`, `missing section "## Affected areas"`, `missing section "## Out of scope"`, `missing section "## Tasks"`},
		},
		"duplicate ID": {
			strings.Replace(goodPlan, "- AC-3:", "- AC-1:", 1),
			[]string{"AC-1 is used more than once"},
		},
		"no criteria": {
			goodPlan[:strings.Index(goodPlan, "<!--")],
			[]string{"no acceptance criteria"},
		},
		"empty text": {
			strings.Replace(goodPlan, "- AC-3: A successful payment sends exactly one email,\n  even when the webhook is delivered twice.\n", "- AC-3:\n", 1),
			[]string{"AC-3 has no text"},
		},
		"duplicate of an empty criterion": {
			strings.Replace(goodPlan, "- AC-3: A successful", "- AC-1:\n- AC-3: A successful", 1),
			[]string{"AC-1 is used more than once"},
		},
		"other list styles": {
			strings.Replace(goodPlan, "- AC-3:", "* AC-3:", 1) + "1. AC-4: numbered\n",
			[]string{`not an acceptance criterion: "* AC-3:`, `not an acceptance criterion: "1. AC-4: numbered"`},
		},
		"no space after the colon": {
			strings.Replace(goodPlan, "- AC-3: ", "- AC-3:", 1),
			[]string{`not an acceptance criterion: "- AC-3:A successful`},
		},
		"heading inside a code fence": {
			strings.Replace(goodPlan, "- AC-1:", "```\n## Context\n```\n\n- AC-1:", 1) + "- AC-1: dup\n",
			[]string{"AC-1 is used more than once"},
		},
		"bad item": {
			strings.Replace(goodPlan, "- AC-3:", "- AC-03:", 1),
			[]string{`not an acceptance criterion: "- AC-03:`},
		},
	}
	for name, c := range cases {
		got := messages(core.ValidatePlan([]byte(c.plan)))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: problems do not include %q:\n%s", name, w, got)
			}
		}
	}
}

func TestValidatePlanFlagsTheEmptyTemplate(t *testing.T) {
	tpl, err := os.ReadFile(filepath.Join("..", "..", ".beforecode", "templates", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := messages(core.ValidatePlan(tpl)); !strings.Contains(got, "AC-1 has no text") {
		t.Errorf("template's empty AC-1 not flagged:\n%s", got)
	}
}

func TestValidatePlanGivesLineNumbers(t *testing.T) {
	problems := core.ValidatePlan([]byte(strings.Replace(goodPlan, "- AC-3:", "- AC-1:", 1)))
	if len(problems) != 1 || problems[0].Line != strings.Count(goodPlan[:strings.Index(goodPlan, "- AC-3:")], "\n")+1 {
		t.Errorf("problems = %+v", problems)
	}
}

// AcceptanceCriteria lists the IDs in the order of plan.md, skipping
// comments, code blocks, items outside the section and broken items.
func TestAcceptanceCriteriaListsTheIDsInOrder(t *testing.T) {
	plan := goodPlan + "\n```\n- AC-7: inside a fence\n```\n- AC-2: last\n- AC-2: twice\n- not a criterion\n\n## Notes\n\n- AC-8: outside the section\n"
	got := core.AcceptanceCriteria([]byte(plan))
	if want := []string{"AC-1", "AC-3", "AC-2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := core.AcceptanceCriteria(nil); len(got) != 0 {
		t.Errorf("no plan.md: got %v", got)
	}
}

// Issue #100 AC-4: Tasks are "- T-<n>: <text>" items of ## Tasks, checked
// like acceptance criteria.
func TestValidatePlanChecksTasks(t *testing.T) {
	cases := map[string]struct {
		plan string
		want []string
	}{
		"not a task":       {strings.Replace(goodPlan, "- T-1: Add", "- Add", 1), []string{`not a task: "- Add the client."; write "- T-<n>: <text>"`}},
		"leading zero":     {strings.Replace(goodPlan, "- T-1:", "- T-01:", 1), []string{`not a task: "- T-01:`}},
		"zero":             {strings.Replace(goodPlan, "- T-1:", "- T-0:", 1), []string{`not a task: "- T-0:`}},
		"duplicate ID":     {strings.Replace(goodPlan, "- T-2:", "- T-1:", 1), []string{"T-1 is used more than once (first on line"}},
		"empty text":       {strings.Replace(goodPlan, "- T-1: Add the client.", "- T-1:", 1), []string{"T-1 has no text"}},
		"other list style": {strings.Replace(goodPlan, "- T-1:", "* T-1:", 1), []string{`not a task: "* T-1:`}},
	}
	for name, c := range cases {
		got := messages(core.ValidatePlan([]byte(c.plan)))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: problems do not include %q:\n%s", name, w, got)
			}
		}
	}
}

func TestValidatePlanAllowsAnEmptyTasksSection(t *testing.T) {
	plan := strings.Replace(goodPlan, "- T-1: Add the client.\n- T-2: Handle the webhook idempotently,\n  keyed by the event ID.\n", "", 1)
	if problems := core.ValidatePlan([]byte(plan)); len(problems) != 0 {
		t.Errorf("problems with an empty ## Tasks:\n%s", messages(problems))
	}
	if got := core.Tasks([]byte(plan)); len(got) != 0 {
		t.Errorf("Tasks = %v, want none", got)
	}
}

// Tasks lists the Tasks in the order of plan.md with their text,
// continuation lines included, skipping comments, fences, broken items,
// duplicates and items outside the section.
func TestTasksListsTheTasksInOrder(t *testing.T) {
	plan := strings.Replace(goodPlan, "- T-1: Add the client.\n",
		"- T-3: Third first.\n<!--\n- T-9: in a comment\n-->\n```\n- T-8: in a fence\n```\n- T-1: Add the client.\n- T-3: again\n- broken\n", 1) +
		"\n## Notes\n\n- T-7: outside the section\n"
	got := core.Tasks([]byte(plan))
	want := []core.Task{
		{ID: "T-3", Text: "Third first."},
		{ID: "T-1", Text: "Add the client."},
		{ID: "T-2", Text: "Handle the webhook idempotently,\nkeyed by the event ID."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("task %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := core.Tasks(nil); len(got) != 0 {
		t.Errorf("no plan.md: got %v", got)
	}
}

func TestValidatePlanFlagsTheEmptyTemplateTask(t *testing.T) {
	tpl, err := os.ReadFile(filepath.Join("..", "..", ".beforecode", "templates", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := messages(core.ValidatePlan(tpl)); !strings.Contains(got, "T-1 has no text") {
		t.Errorf("template's empty T-1 not flagged:\n%s", got)
	}
}

// A Task whose text starts on a continuation line has text, and a code
// block indented under a Task belongs to it.
func TestTaskTextFromContinuationLinesAndIndentedCode(t *testing.T) {
	plan := strings.Replace(goodPlan, "- T-1: Add the client.\n",
		"- T-1:\n  Add the client.\n  ```\n  ## not a heading\n\n  go get stripe\n  ```\n  Then wire it.\n", 1)
	if problems := core.ValidatePlan([]byte(plan)); len(problems) != 0 {
		t.Errorf("problems:\n%s", messages(problems))
	}
	got := core.Tasks([]byte(plan))
	want := "Add the client.\n```\n## not a heading\n\ngo get stripe\n```\nThen wire it."
	if len(got) != 2 || got[0].Text != want {
		t.Errorf("tasks = %q", got)
	}
}

// Issue #102 AC-3: the acceptance criteria with their text, for the plan
// check.
func TestCriteriaHaveTheirText(t *testing.T) {
	got := core.Criteria([]byte(goodPlan))
	if len(got) != 2 || got[0] != (core.Criterion{ID: "AC-1", Text: "With an invalid card the API returns 402."}) ||
		got[1] != (core.Criterion{ID: "AC-3", Text: "A successful payment sends exactly one email,\neven when the webhook is delivered twice."}) {
		t.Errorf("Criteria = %q", got)
	}
}

// Issue #102 AC-3: the text of Out of scope, without HTML comments, its
// heading matched without regard to case, headings in fences ignored.
func TestOutOfScope(t *testing.T) {
	plan := "## Context\n\n```\n## Out of scope\nin a fence\n```\n\n## out of scope\n\n<!-- What must not change. -->\n\nRefunds.\n\n```\n## not a heading\n```\n\nCurrencies.\n\n## Tasks\n\n- T-1: x\n"
	if got := core.OutOfScope([]byte(plan)); got != "Refunds.\n\n```\n## not a heading\n```\n\nCurrencies." {
		t.Errorf("OutOfScope = %q", got)
	}
	tpl, err := os.ReadFile(filepath.Join("..", "..", ".beforecode", "templates", "plan.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := core.OutOfScope(tpl); got != "" {
		t.Errorf("the template's comment counts as Out of scope: %q", got)
	}
}
