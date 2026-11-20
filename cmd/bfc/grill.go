package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	beforecode "github.com/admSla99/BeforeCode"
	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
)

// grillSkill is the skill bfc prime grill hands the agent.
const grillSkill = beforecode.SkillsDir + "/grill-intent/SKILL.md"

// grillView is what bfc prime grill prints, gathered by gatherGrill.
type grillView struct {
	Skill []byte // the repository's grill-intent, else the built-in one
	// Plan is the Current plan (Current) or the one given; nil when there
	// is none, and Note says why.
	Plan     *index.Plan
	Current  bool
	Messages []core.Message // all Messages of the plan, oldest first
	Note     string
}

// runPrimeGrill is bfc prime grill [id]: the grill-intent skill, then the
// plan's Intent and its Grill so far, so that a Grill can go on in a new
// session. Like bfc prime it never fails over plans.
func runPrimeGrill(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("bfc prime grill takes at most one plan ID")
	}
	return renderGrill(cmd.OutOrStdout(), gatherGrill(cmd, args))
}

func gatherGrill(cmd *cobra.Command, args []string) grillView {
	ctx := cmd.Context()
	v := grillView{Skill: builtInSkill(grillSkill)}
	s, err := openStore(cmd)
	if err != nil {
		v.Note = err.Error()
		return v
	}
	if skill, err := readDefault(s.Root(), grillSkill, beforecode.Skills); err == nil {
		v.Skill = skill
	}
	if s, err = openPlans(cmd, forReading); err != nil {
		v.Note = err.Error()
		return v
	}
	var p index.Plan
	if len(args) == 1 {
		p, err = resolvePlan(ctx, s, args[0])
	} else {
		p, err = currentPlan(ctx, s)
		v.Current = true
	}
	switch {
	case errors.Is(err, errNoCurrentPlan):
		v.Note = "There is no Current plan."
		return v
	case err != nil:
		v.Note = err.Error()
		return v
	}
	v.Plan = &p
	if v.Messages, err = readMessages(ctx, s, p.ID); err != nil {
		v.Note = "The Grill so far cannot be read: " + err.Error()
	}
	return v
}

// builtInSkill returns the skill at rel as built into bfc.
func builtInSkill(rel string) []byte {
	data, _ := beforecode.Skills.ReadFile(rel) // tested to exist
	return data
}

// renderGrill prints v as Markdown for an agent.
func renderGrill(w io.Writer, v grillView) error {
	var b strings.Builder
	b.WriteString(withoutFrontMatter(string(v.Skill)))
	if v.Plan == nil {
		fmt.Fprintf(&b, "\n## Plan\n\n%s\n\nName the plan to grill: `bfc prime grill <id>`; `bfc list` shows the plans.\n", v.Note)
		_, err := io.WriteString(w, b.String())
		return err
	}
	p := v.Plan
	which := "Plan"
	if v.Current {
		which = "Current plan"
	}
	author := p.Status.Author
	fmt.Fprintf(&b, "\n## %s: %s %s\n\n", which, p.ShortID, p.Title)
	fmt.Fprintf(&b, "- State: %s\n", p.Status.State)
	fmt.Fprintf(&b, "- Author: %s <%s>, whom you grill.\n", orDash(author.Name), orDash(author.Email))
	fmt.Fprintf(&b, "- Record a Question: `bfc ask %s --to %s --ref intent.md --json -m \"<question>\"`\n", p.ShortID, orDash(author.Email))
	b.WriteString("- Record the answer: `bfc reply <ULID> --verbatim -m \"<their words>\"`\n")

	intent := string(p.Intent)
	if !strings.HasSuffix(intent, "\n") {
		intent += "\n"
	}
	fence := fenceFor(intent)
	fmt.Fprintf(&b, "\n### intent.md\n\n%smarkdown\n%s%s\n", fence, intent, fence)

	b.WriteString("\n### The Grill so far\n\n")
	if v.Note != "" {
		b.WriteString(v.Note + "\n")
	}
	writeGrill(&b, v.Messages)
	_, err := io.WriteString(w, b.String())
	return err
}

// writeGrill prints each Question of msgs with its answers, in the order
// they were asked. Messages that neither ask nor answer are left out.
func writeGrill(b *strings.Builder, msgs []core.Message) {
	open := core.OpenQuestions(msgs)
	n := 0
	for _, q := range msgs {
		if !q.IsQuestion() {
			continue
		}
		n++
		status := "answered"
		if slices.ContainsFunc(open, func(o core.Message) bool { return o.ID == q.ID }) {
			status = "open"
		}
		fmt.Fprintf(b, "%d. Question %s%s to %s (%s):\n%s", n, q.ID, refSuffix(q.Ref), strings.Join(q.To, ", "), status, indent(q.Body))
		for _, r := range msgs {
			if r.ReplyTo != q.ID.String() {
				continue
			}
			by := orDash(r.Author.Name)
			if r.Agent != "" {
				by += " (through " + r.Agent + ")"
			}
			fmt.Fprintf(b, "   Reply %s by %s:\n%s", r.ID, by, indent(r.Body))
		}
	}
	if n == 0 {
		b.WriteString("No Questions yet: start with the first round.\n")
	}
}

// indent returns text indented to sit under a list item, ending in a
// newline.
func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	return "   > " + strings.Join(lines, "\n   > ") + "\n"
}

// withoutFrontMatter returns a SKILL.md without its YAML front matter,
// which is for the harness that loads skills, not for the agent.
func withoutFrontMatter(doc string) string {
	if rest, ok := strings.CutPrefix(strings.ReplaceAll(doc, "\r\n", "\n"), "---\n"); ok {
		if _, body, found := strings.Cut(rest, "\n---\n"); found {
			return strings.TrimLeft(body, "\n")
		}
	}
	return doc
}
