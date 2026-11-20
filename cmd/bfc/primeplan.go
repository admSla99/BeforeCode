package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
)

// planSkill is the skill bfc prime plan hands the agent.
const planSkill = beforecode.SkillsDir + "/write-plan/SKILL.md"

// runPrimePlan is bfc prime plan [id]: the write-plan skill, then what
// plan.md is written from: the template, the plan's Intent and its Grill.
// Like bfc prime it never fails over plans.
func runPrimePlan(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("bfc prime plan takes at most one plan ID")
	}
	v := gatherTopic(cmd, args, planSkill)
	v.Template, _ = beforecode.Templates.ReadFile(planTemplate) // tested to exist
	if s, err := openStore(cmd); err == nil {
		if tpl, err := readTemplate(s.Root(), planTemplate); err == nil {
			v.Template = tpl
		}
	}
	return renderPlanTopic(cmd.OutOrStdout(), v)
}

// renderPlanTopic prints v as Markdown for an agent.
func renderPlanTopic(w io.Writer, v topicView) error {
	var b strings.Builder
	if !writeTopicHead(&b, v, "plan") {
		_, err := io.WriteString(w, b.String())
		return err
	}
	p := v.Plan
	fmt.Fprintf(&b, "- Author: %s <%s>\n", orDash(p.Status.Author.Name), orDash(p.Status.Author.Email))
	written := p.Plan != nil && !sameText(p.Plan, v.Template)
	if written {
		fmt.Fprintf(&b, "- plan.md is already written; it is shown below. Ask the user before you change it; once they agree, store your version with `bfc edit %s --stdin --force`.\n", p.ShortID)
	}
	fmt.Fprintf(&b, "- Store plan.md: `bfc edit %s --stdin`, with the document on stdin\n", p.ShortID)
	fmt.Fprintf(&b, "- Ask what is missing: `bfc ask %s --to %s --ref intent.md --json -m \"<question>\"`, then `bfc reply <id> --verbatim -m \"<their words>\"`\n", p.ShortID, authorAddress(p))
	writeFenced(&b, "plan.md template", v.Template)
	writeFenced(&b, "intent.md", p.Intent)
	writeGrillSection(&b, v)
	if written {
		writeFenced(&b, "plan.md as written", p.Plan)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
