package main

import (
	"fmt"
	"os"
	"regexp"
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

var (
	// atxHeading is a heading line "# …" to "##### …", indented by up to
	// three spaces; a level 6 heading stays as deep as Markdown goes.
	atxHeading = regexp.MustCompile(`^( {0,3})(#{1,5})(\s|$)`)
	// setextUnderline turns the paragraph line above it into a heading:
	// "===" level 1, "---" level 2.
	setextUnderline = regexp.MustCompile(`^ {0,3}(=+|-+)\s*$`)
	// fenceOpen opens a code block, which only the same character, at
	// least as many times, closes.
	fenceOpen = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

// shiftHeadings moves every heading of a Markdown document one level
// down: ATX headings get one more #, setext headings become ATX headings
// one level deeper. Code fences and HTML comments are left as they are.
func shiftHeadings(doc string) string {
	in := strings.Split(doc, "\n")
	out := make([]string, 0, len(in))
	fence, inComment := "", false
	for i, l := range in {
		switch {
		case fence != "":
			if m := fenceOpen.FindStringSubmatch(l); m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), m[1][:1])) == "" {
				fence = ""
			}
		case inComment:
			inComment = !strings.Contains(l, "-->")
		case strings.HasPrefix(strings.TrimSpace(l), "<!--"):
			inComment = !strings.Contains(l, "-->")
		case fenceOpen.MatchString(l):
			fence = fenceOpen.FindStringSubmatch(l)[1]
		case atxHeading.MatchString(l):
			l = atxHeading.ReplaceAllString(l, "$1#$2$3")
		case setextUnderline.MatchString(l) && len(out) > 0 && i > 0 && isParagraphLine(in[i-1]):
			level := "## "
			if strings.Contains(l, "-") {
				level = "### "
			}
			out[len(out)-1] = level + strings.TrimSpace(out[len(out)-1])
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// isParagraphLine reports whether a line can be the text of a setext
// heading: not blank, not a list item, a heading, a quote or a fence.
func isParagraphLine(l string) bool {
	t := strings.TrimSpace(l)
	return t != "" && !listItemStart.MatchString(t) && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, ">") && !fenceOpen.MatchString(l)
}

// listItemStart is the start of a Markdown list item.
var listItemStart = regexp.MustCompile(`^(?:[-*+]|[0-9]+[.)])(?:\s|$)`)

func writeExportTasks(b *strings.Builder, v exportView) {
	tasks := core.TaskViews(v.Plan.Status, v.Plan.Plan)
	if len(tasks) == 0 && len(v.Handoffs) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, t := range tasks {
		listed[t.ID] = true
	}
	b.WriteString("\n## Tasks\n\n")
	for _, t := range tasks {
		state := string(t.State)
		if t.State != core.TaskOpen {
			state += ", " + throughAgent(orDash(t.Holder.Name), t.Agent)
		}
		fmt.Fprintf(b, "- **%s** (%s): %s\n", t.ID, state, strings.ReplaceAll(t.Text, "\n", "\n  "))
		for _, h := range v.Handoffs {
			if h.Task == t.ID && v.HandoffEvents[h.ID] != "" {
				writeExportHandoff(b, h)
			}
		}
	}
	// Notes that change nothing, kept for the record.
	for _, h := range v.Handoffs {
		switch {
		case v.HandoffEvents[h.ID] == "":
			fmt.Fprintf(b, "- **%s**, a note without its Event:\n", h.Task)
			writeExportHandoff(b, h)
		case !listed[h.Task]:
			fmt.Fprintf(b, "- **%s**, a Task plan.md no longer has:\n", h.Task)
			writeExportHandoff(b, h)
		}
	}
}

func writeExportHandoff(b *strings.Builder, h core.Handoff) {
	what := "released"
	if h.Done {
		what = "completed"
	}
	fmt.Fprintf(b, "  - Handoff `%s`, %s by %s, %s:\n\n", h.ID, what, throughAgent(orDash(h.Author.Name), h.Agent), utc(h.At))
	b.WriteString(quote(shiftHeadings(h.Note), "    ") + "\n")
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
			fmt.Fprintf(b, "\nAcknowledged by %s, %s:\n\n%s\n", orDash(a.Author.Name), utc(a.At), quote(shiftHeadings(a.Body), ""))
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
		by := orDash(m.Author.Name)
		if m.Author.Email != "" {
			by += " <" + m.Author.Email + ">"
		}
		fields := []string{"`" + m.ID.String() + "`", "**" + throughAgent(by, m.Agent) + "**", utc(m.At)}
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
		fmt.Fprintf(b, "%s- %s\n\n%s\n\n", pad, strings.Join(fields, " · "), quote(shiftHeadings(m.Body), pad+"  "))
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
