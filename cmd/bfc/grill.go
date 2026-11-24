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

// topicView is what a topic of bfc prime prints, gathered by gatherTopic:
// a skill, then one plan with its Messages.
type topicView struct {
	Skill []byte // the repository's copy of the skill, else the built-in one
	// Template is the plan.md template, for the plan topic.
	Template []byte
	// Plan is the Current plan (Current) or the one given; nil when there
	// is none, and Note says why.
	Plan     *index.Plan
	Current  bool
	Messages []core.Message // all Messages of the plan, oldest first
	// Unread says why the Messages cannot be read.
	Unread string
	Note   string
	root   string // the repository's root, "" outside one
}

// runPrimeGrill is bfc prime grill [id]: the grill-intent skill, then the
// plan's Intent and its Grill so far, so that a Grill can go on in a new
// session. Like bfc prime it never fails over plans.
func runPrimeGrill(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return errors.New("bfc prime grill takes at most one plan ID")
	}
	return renderGrill(cmd.OutOrStdout(), gatherTopic(cmd, args, grillSkill))
}

// gatherSkill returns a topicView with only the skill at skill: the
// repository's copy, else the one built into bfc.
func gatherSkill(cmd *cobra.Command, skill string) topicView {
	v := topicView{Skill: builtInSkill(skill)}
	if root, err := openStore(cmd); err == nil {
		v.root = root.Root()
		if data, err := readDefault(v.root, skill, beforecode.Skills); err == nil {
			v.Skill = data
		}
	}
	return v
}

// gatherTopic collects the skill at skill (the repository's copy wins) and
// the plan named by args or the Current plan, with its Messages. Problems
// become notes, like in bfc prime.
func gatherTopic(cmd *cobra.Command, args []string, skill string) topicView {
	ctx := cmd.Context()
	v := gatherSkill(cmd, skill)
	s, setup := openPrime(cmd)
	if setup != "" {
		v.Note = setup
		return v
	}
	p, current, err := primePlan(ctx, s, args)
	v.Current = current
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
		v.Unread = err.Error()
	}
	return v
}

// builtInSkill returns the skill at rel as built into bfc.
func builtInSkill(rel string) []byte {
	data, _ := beforecode.Skills.ReadFile(rel) // tested to exist
	return data
}

// renderGrill prints v as Markdown for an agent.
func renderGrill(w io.Writer, v topicView) error {
	var b strings.Builder
	if !writeTopicHead(&b, v, "grill") {
		_, err := io.WriteString(w, b.String())
		return err
	}
	p := v.Plan
	fmt.Fprintf(&b, "- Author: %s <%s>, whom you grill.\n", orDash(p.Status.Author.Name), orDash(p.Status.Author.Email))
	fmt.Fprintf(&b, "- Record a Question: `bfc ask %s --to %s --ref intent.md --json -m \"<question>\"`; keep the `id` it prints\n", p.ShortID, authorAddress(p))
	b.WriteString("- Record the answer: `bfc reply <id> --verbatim -m \"<their words>\"`\n")
	writeFenced(&b, "intent.md", p.Intent)
	writeGrillSection(&b, v)
	_, err := io.WriteString(w, b.String())
	return err
}

// writeTopicHead writes the skill and the heading of the plan of the topic
// bfc prime <topic>, and reports whether there is a plan; without one it
// writes the note and how to name a plan.
func writeTopicHead(b *strings.Builder, v topicView, topic string) bool {
	b.WriteString(withoutFrontMatter(string(v.Skill)))
	if v.Plan == nil {
		name := fmt.Sprintf("Name the plan: `bfc prime %s <id>`; `bfc list` shows the plans.", topic)
		if topic == "check" {
			// A pull request names its own plan.
			name = "Find the pull request's plan with `bfc check find` (the steps above), then run `bfc prime check <id>`."
		}
		fmt.Fprintf(b, "\n## Plan\n\n%s\n\n%s\n", v.Note, name)
		return false
	}
	which := "Plan"
	if v.Current {
		which = "Current plan"
	}
	fmt.Fprintf(b, "\n## %s: %s %s\n\n- State: %s\n", which, v.Plan.ShortID, v.Plan.Title, v.Plan.Status.State)
	return true
}

// authorAddress is the email of the plan's author for --to, or a
// placeholder when the author has none.
func authorAddress(p *index.Plan) string {
	if p.Status.Author.Email == "" {
		return "<author email>"
	}
	return p.Status.Author.Email
}

// writeFenced writes a level-3 heading and doc in a Markdown fence.
func writeFenced(b *strings.Builder, heading string, doc []byte) {
	text := string(doc)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	fence := fenceFor(text)
	fmt.Fprintf(b, "\n### %s\n\n%smarkdown\n%s%s\n", heading, fence, text, fence)
}

// writeGrillSection writes the Questions of the plan with their answers.
func writeGrillSection(b *strings.Builder, v topicView) {
	b.WriteString("\n### The Grill so far\n\n")
	if v.Unread != "" {
		b.WriteString("The Grill so far cannot be read: " + v.Unread + "\n")
	}
	writeGrill(b, v.Messages)
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
			fmt.Fprintf(b, "   Reply %s by %s:\n%s", r.ID, throughAgent(orDash(r.Author.Name), r.Agent), indent(r.Body))
		}
	}
	if n == 0 {
		b.WriteString("No Questions yet.\n")
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
	text := strings.TrimPrefix(strings.ReplaceAll(doc, "\r\n", "\n"), "\ufeff")
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if _, body, found := strings.Cut(rest+"\n", "\n---\n"); found {
			if body = strings.Trim(body, "\n"); body != "" {
				body += "\n"
			}
			return body
		}
	}
	return doc
}
