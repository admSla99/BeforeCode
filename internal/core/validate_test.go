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

- Add the client.

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
