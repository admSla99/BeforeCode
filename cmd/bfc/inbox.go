package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newInboxCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List what waits on you: Findings, reviews and Questions",
		Long: `List what waits on the git author of this clone, as the TUI's Inbox and
Prime's Waiting on you do: the Findings of plan checks on your plans that are
not acknowledged, the plans in review you may approve now, and the open
Questions addressed to you. It reads only this clone; bfc pull brings what
others wrote.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			me, w, err := loadWaiting(cmd.Context(), s)
			if err != nil {
				return err
			}
			if asJSON {
				return printInboxJSON(cmd.OutOrStdout(), me, w)
			}
			return printInbox(cmd.OutOrStdout(), w)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON for scripts and agents")
	return cmd
}

// loadWaiting returns the git author of the clone and what waits on them.
func loadWaiting(ctx context.Context, s *store.Store) (core.Person, core.Waiting, error) {
	me, err := currentUser(ctx, s)
	if err != nil {
		return core.Person{}, core.Waiting{}, err
	}
	views, err := loadPlans(ctx, s)
	if err != nil {
		return core.Person{}, core.Waiting{}, err
	}
	return me, gatherWaiting(ctx, s, me, views), nil
}

// gatherWaiting reads the Messages and plan checks of the plans that can
// have something waiting and returns what waits on me (core.WaitingOn). A
// plan whose files cannot be read is left out, so one broken plan does not
// hide the rest; bfc doctor reports it.
func gatherWaiting(ctx context.Context, s *store.Store, me core.Person, views []index.Plan) core.Waiting {
	var work []core.PlanWork
	for _, v := range views {
		if !core.Waits(v.Status.State) {
			continue
		}
		msgs, err := readMessages(ctx, s, v.ID)
		if err != nil {
			continue
		}
		checks, err := planChecks(ctx, s, v.ID, msgs)
		if err != nil {
			continue
		}
		work = append(work, core.PlanWork{ID: v.ID, ShortID: v.ShortID, Title: v.Title, Status: v.Status, Messages: msgs, Checks: checks})
	}
	return core.WaitingOn(me, work)
}

// printInbox prints the three lists, each with a heading and a count, or
// one line when nothing waits.
func printInbox(w io.Writer, in core.Waiting) error {
	if in.Empty() {
		_, err := fmt.Fprintln(w, "Nothing waits on you.")
		return err
	}
	var b strings.Builder
	section := func(name string, n int) {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s (%d)\n", name, n)
	}
	if len(in.Findings) > 0 {
		section("Findings", len(in.Findings))
		for _, f := range in.Findings {
			fmt.Fprintf(&b, "  %s  PR #%d  %s  (%s)\n", f.ShortID, f.Finding.PR, f.Finding.Text(), f.Title)
		}
	}
	if len(in.Reviews) > 0 {
		section("Reviews", len(in.Reviews))
		for _, r := range in.Reviews {
			fmt.Fprintf(&b, "  %s  %s\n", r.ShortID, r.Title)
		}
	}
	if len(in.Questions) > 0 {
		section("Questions", len(in.Questions))
		for _, q := range in.Questions {
			fmt.Fprintf(&b, "  %s  %s  from %s%s: %s\n", q.ShortID, q.Message.ID, q.Message.Author.Email, refSuffix(q.Message.Ref), firstLine(q.Message.Body))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// inboxJSON is the --json shape of bfc inbox, documented in docs/design.md.
type inboxJSON struct {
	Identity  core.Person         `json:"identity"`
	Findings  []inboxFindingJSON  `json:"findings"`
	Reviews   []inboxPlanJSON     `json:"reviews"`
	Questions []inboxQuestionJSON `json:"questions"`
}

type inboxPlanJSON struct {
	Plan    string `json:"plan"`
	ShortID string `json:"short_id"`
	Title   string `json:"title"`
}

type inboxFindingJSON struct {
	inboxPlanJSON
	PR         int    `json:"pr"`
	Check      string `json:"check"`
	Criterion  string `json:"criterion,omitempty"`
	OutOfScope string `json:"out_of_scope,omitempty"`
}

type inboxQuestionJSON struct {
	inboxPlanJSON
	ID     string      `json:"id"`
	Author core.Person `json:"author"`
	At     string      `json:"at"`
	To     []string    `json:"to"`
	Ref    string      `json:"ref,omitempty"`
	Body   string      `json:"body"`
}

func toInboxPlanJSON(r core.PlanRef) inboxPlanJSON {
	return inboxPlanJSON{Plan: r.ID.String(), ShortID: r.ShortID, Title: r.Title}
}

func printInboxJSON(w io.Writer, me core.Person, in core.Waiting) error {
	out := inboxJSON{
		Identity: me, Findings: []inboxFindingJSON{}, Reviews: []inboxPlanJSON{}, Questions: []inboxQuestionJSON{},
	}
	for _, f := range in.Findings {
		out.Findings = append(out.Findings, inboxFindingJSON{
			inboxPlanJSON: toInboxPlanJSON(f.PlanRef), PR: f.Finding.PR, Check: f.Finding.Check.String(),
			Criterion: f.Finding.Criterion, OutOfScope: f.Finding.OutOfScope,
		})
	}
	for _, r := range in.Reviews {
		out.Reviews = append(out.Reviews, toInboxPlanJSON(r))
	}
	for _, q := range in.Questions {
		m := q.Message
		out.Questions = append(out.Questions, inboxQuestionJSON{
			inboxPlanJSON: toInboxPlanJSON(q.PlanRef), ID: m.ID.String(), Author: m.Author,
			At: m.At.UTC().Format(time.RFC3339), To: m.To, Ref: m.Ref, Body: m.Body,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
