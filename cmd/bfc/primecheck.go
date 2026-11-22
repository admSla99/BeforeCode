package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
	"github.com/admSla99/BeforeCode/internal/core"
)

// checkSkill is the skill bfc prime check hands the agent.
const checkSkill = beforecode.SkillsDir + "/plan-check/SKILL.md"

// runPrimeCheck is bfc prime check [id]: the plan-check skill, then what a
// pull request is checked against: the plan's acceptance criteria, its Out
// of scope, plan.md and the shape of the result bfc check record reads
// (ADR 0014). Like bfc prime it never fails over plans and stays within
// primeLimit.
func runPrimeCheck(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("bfc prime check takes at most one plan ID")
	}
	return renderCheckTopic(cmd.OutOrStdout(), gatherTopic(cmd, args, checkSkill))
}

// renderCheckTopic prints v as Markdown for an agent, at most primeLimit
// bytes (see fitDoc): plan.md is cut to fit, and the result shape always
// ends the output.
func renderCheckTopic(w io.Writer, v topicView) error {
	var b strings.Builder
	if !writeTopicHead(&b, v, "check") {
		_, err := io.WriteString(w, b.String())
		return err
	}
	p := v.Plan
	if p.Plan == nil {
		fmt.Fprintf(&b, "\n%s has no plan.md, so there is nothing to check the pull request against. Say so in the summary and report no criteria.\n", p.ShortID)
		_, err := io.WriteString(w, b.String())
		return err
	}
	criteria := core.Criteria(p.Plan)
	b.WriteString("\n### Acceptance criteria\n\n")
	if len(criteria) == 0 {
		b.WriteString("plan.md has no well-formed acceptance criteria.\n")
	}
	for _, c := range criteria {
		fmt.Fprintf(&b, "- %s: %s\n", c.ID, strings.ReplaceAll(c.Text, "\n", "\n  "))
	}
	b.WriteString("\n### Out of scope\n\n")
	if out := core.OutOfScope(p.Plan); out != "" {
		b.WriteString(out + "\n")
	} else {
		b.WriteString("plan.md says nothing under `## Out of scope`.\n")
	}
	b.WriteString("\n### plan.md\n\n")
	_, err := io.WriteString(w, fitDoc(b.String(), p.Plan, resultShape(criteria), "plan.md", p.ShortID))
	return err
}

// resultShape is the result an agent ends the check with, for the criteria
// of plan.md, as bfc check record reads it.
func resultShape(criteria []core.Criterion) string {
	var b strings.Builder
	b.WriteString("\n### Result\n\nEnd with this JSON object in a `json` fence, and nothing after it. Give every criterion above once, each `met`, `not-met` or `cannot-verify`; `out_of_scope` is `[]` when there are none.\n\n```json\n{\n  \"criteria\": {")
	for i, c := range criteria {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n    %q: \"<status>\"", c.ID)
	}
	b.WriteString("\n  },\n  \"out_of_scope\": [\"<one sentence per out-of-scope change>\"],\n  \"summary\": \"<the Markdown summary for the pull request>\"\n}\n```\n")
	return b.String()
}
