package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
)

func newExportCmd() *cobra.Command {
	var outFile string
	cmd := &cobra.Command{
		Use:   "export <id> [-o <file>]",
		Short: "Write a plan as one readable Markdown document",
		Long: `Write one Markdown document of a plan, for a reviewer without bfc or for an
archive: a header with its IDs, state, author, Approvals, labels and last
activity, then intent.md, plan.md, the Tasks with their Handoffs, the plan
checks of each pull request with their Findings and Acknowledgements, and
the Discussion. Front matter is left out and the documents' headings move
one level down, so the export has one title. It prints to stdout, or to the
file of -o. It reads only this clone and writes nothing to the plans ref.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			v, err := resolvePlan(ctx, s, args[0])
			if err != nil {
				return err
			}
			msgs, err := readMessages(ctx, s, v.ID)
			if err != nil {
				return err
			}
			checks, err := planChecks(ctx, s, v.ID, msgs)
			if err != nil {
				return err
			}
			hs, events, err := planHandoffs(ctx, s, v)
			if err != nil {
				return err
			}
			doc := renderExport(exportView{Plan: v, Messages: msgs, Checks: checks, Handoffs: hs, HandoffEvents: events})
			if outFile != "" {
				return os.WriteFile(outFile, []byte(doc), 0o644)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), doc)
			return err
		},
	}
	cmd.Flags().StringVarP(&outFile, "output", "o", "", "write the export to this file instead of stdout")
	return cmd
}

// exportView is everything bfc export prints of one plan.
type exportView struct {
	Plan          index.Plan
	Messages      []core.Message
	Checks        []core.PullRequestCheck
	Handoffs      []core.Handoff
	HandoffEvents map[ulid.ULID]string // Handoff -> its Event, see planHandoffs
}

// renderExport returns the export of v: GitHub-flavoured Markdown with one
// # title. Times are in UTC, so the same ref gives the same text.
func renderExport(v exportView) string {
	p := v.Plan
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: %s\n\n", p.ShortID, p.Title)
	fmt.Fprintf(&b, "- **ID:** `%s`\n", p.ID)
	fmt.Fprintf(&b, "- **State:** %s\n", p.Status.State)
	fmt.Fprintf(&b, "- **Author:** %s <%s>\n", orDash(p.Status.Author.Name), orDash(p.Status.Author.Email))
	if p.Status.RequiredApprovals > 0 {
		fmt.Fprintf(&b, "- **Approvals:** %d of %d", len(p.Status.Approvers), p.Status.RequiredApprovals)
		if len(p.Status.Approvers) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(p.Status.Approvers, ", "))
		}
		b.WriteString("\n")
	}
	if len(p.Labels) > 0 {
		fmt.Fprintf(&b, "- **Labels:** %s\n", strings.Join(p.Labels, ", "))
	}
	fmt.Fprintf(&b, "- **Last activity:** %s\n", utc(p.LastActivity))

	writeExportDoc(&b, "Intent", p.Intent, "intent.md")
	writeExportDoc(&b, "Plan", p.Plan, "plan.md")
	writeExportTasks(&b, v)
	writeExportChecks(&b, v.Checks)
	writeExportDiscussion(&b, v.Messages)
	return b.String()
}

// writeExportDoc writes a document under its own heading, without front
// matter and with its headings one level down.
func writeExportDoc(b *strings.Builder, title string, doc []byte, name string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
	if doc == nil {
		fmt.Fprintf(b, "_No %s yet._\n", name)
		return
	}
	text := strings.TrimSpace(shiftHeadings(withoutFrontMatter(strings.ReplaceAll(string(doc), "\r\n", "\n"))))
	b.WriteString(text + "\n")
}

// shiftHeadings moves every ATX heading of a Markdown document one level
// down, leaving fenced code blocks as they are.
func shiftHeadings(doc string) string {
	lines := strings.Split(doc, "\n")
	inFence := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(l, "#") && strings.HasPrefix(strings.TrimLeft(l, "#"), " ") && len(l)-len(strings.TrimLeft(l, "#")) < 6 {
			lines[i] = "#" + l
		}
	}
	return strings.Join(lines, "\n")
}

func writeExportTasks(b *strings.Builder, v exportView) {
	tasks := core.TaskViews(v.Plan.Status, v.Plan.Plan)
	if len(tasks) == 0 {
		return
	}
	b.WriteString("\n## Tasks\n\n")
	for _, t := range tasks {
		state := string(t.State)
		if t.State != core.TaskOpen {
			state += ", " + throughAgent(orDash(t.Holder.Name), t.Agent)
		}
		fmt.Fprintf(b, "- **%s** (%s): %s\n", t.ID, state, strings.ReplaceAll(t.Text, "\n", "\n  "))
		for _, h := range v.Handoffs {
			if h.Task != t.ID || v.HandoffEvents[h.ID] == "" {
				continue
			}
			what := "released"
			if h.Done {
				what = "completed"
			}
			fmt.Fprintf(b, "  - Handoff, %s by %s, %s:\n\n", what, throughAgent(orDash(h.Author.Name), h.Agent), utc(h.At))
			b.WriteString(quote(shiftHeadings(h.Note), "    ") + "\n")
		}
	}
}

func writeExportChecks(b *strings.Builder, checks []core.PullRequestCheck) {
	if len(checks) == 0 {
		return
	}
	b.WriteString("\n## Checks\n")
	for _, p := range checks {
		c := p.Check
		fmt.Fprintf(b, "\n### Pull request #%d\n\n", c.PR)
		fmt.Fprintf(b, "Newest check `%s`, %s by %s.", c.ID, utc(c.At), throughAgent(orDash(c.Author.Name), c.Agent))
		if p.Stale {
			b.WriteString(" It checked an older `plan.md`.")
		}
		b.WriteString("\n\n")
		for _, f := range p.Findings {
			fmt.Fprintf(b, "- Finding: %s\n", f.Text())
		}
		for _, id := range p.Unverified {
			fmt.Fprintf(b, "- Cannot verify: %s\n", id)
		}
		if len(p.Findings) == 0 {
			b.WriteString("- No Findings.\n")
		}
		if a := p.Acknowledgement; a != nil {
			fmt.Fprintf(b, "\nAcknowledged by %s, %s:\n\n%s\n", orDash(a.Author.Name), utc(a.At), quote(a.Body, ""))
		}
	}
}

func writeExportDiscussion(b *strings.Builder, msgs []core.Message) {
	if len(msgs) == 0 {
		return
	}
	b.WriteString("\n## Discussion\n\n")
	for _, m := range core.Discussion(msgs) {
		pad := strings.Repeat("  ", m.Depth)
		fields := []string{"**" + throughAgent(orDash(m.Author.Name), m.Agent) + "**", utc(m.At)}
		if m.IsQuestion() {
			status := "open Question"
			if m.Answered {
				status = "answered Question"
			}
			fields = append(fields, status+" to "+strings.Join(m.To, ", "))
		}
		if m.Ref != "" {
			fields = append(fields, "about `"+m.Ref+"`")
		}
		fmt.Fprintf(b, "%s- %s\n\n%s\n\n", pad, strings.Join(fields, " · "), quote(m.Body, pad+"  "))
	}
}

// quote returns text as a Markdown block quote, each line indented by pad.
func quote(text, pad string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(pad+"> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// utc formats a time for the export, "-" when unknown.
func utc(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
