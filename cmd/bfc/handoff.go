package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newHandoffCmd() *cobra.Command {
	var text string
	var done, asJSON bool
	cmd := &cobra.Command{
		Use:   "handoff [<plan-id>] T-n [-m <note>] [--done]",
		Short: "Stop working on a Task you hold, with a note for the next one",
		Long: `Stop working on a Task you hold and leave a Handoff (ADR 0013): a note
with what is done and what comes next. With --done the Task is completed;
without it the Task is released and Paused, and anyone may claim it again
and read the note first. Without a plan ID it is a Task of the Current plan.
The note comes from -m or, without it, from stdin.

Only the holder of the Task may hand it off; agents may. Nothing is pushed:
run bfc push so the next one sees it.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, task, err := claimArgs(args)
			if err != nil {
				return err
			}
			if task == "" {
				return errors.New("name the Task to hand off: bfc handoff [<plan-id>] T-n -m <note>")
			}
			note, err := messageText(cmd, text)
			if err != nil {
				return err
			}
			return runHandoff(cmd, query, task, note, done, asJSON)
		},
	}
	cmd.Flags().StringVarP(&text, "message", "m", "", "the note: what is done and what comes next (default: read from stdin)")
	cmd.Flags().BoolVar(&done, "done", false, "the Task is completed; without it the Task is released and Paused")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the Handoff as JSON for scripts and agents")
	return cmd
}

func runHandoff(cmd *cobra.Command, query, task, note string, done, asJSON bool) error {
	ctx := cmd.Context()
	s, err := openPlans(cmd, forWriting)
	if err != nil {
		return err
	}
	var v index.Plan
	if query == "" {
		v, err = currentPlan(ctx, s)
	} else {
		v, err = resolvePlan(ctx, s, query)
	}
	if err != nil {
		return err
	}
	if err := core.CheckWritable(v.Status); err != nil {
		return err
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return err
	}
	if err := core.CheckHandoff(v.Status, v.Plan, task, actor); err != nil {
		return err
	}
	now := time.Now()
	entropy := ulid.Monotonic(rand.Reader, 0)
	h := core.Handoff{
		ID: ulid.MustNew(ulid.Timestamp(now), entropy), Author: actor, At: now, Agent: insideAgent(cmd),
		Task: task, Done: done, Note: note,
	}
	e := core.Event{
		ID: ulid.MustNew(ulid.Timestamp(now), entropy), Type: core.Released, Plan: v.ID,
		Author: actor, At: now, Agent: h.Agent, Task: task, Handoff: h.ID.String(),
	}
	if done {
		e.Type = core.Completed
	}
	data := core.EncodeHandoff(h)
	files := map[string][]byte{
		core.HandoffPath(v.ID, h.ID):                              data,
		core.PlanDir(v.ID) + "/events/" + e.ID.String() + ".yaml": core.EncodeEvent(e),
	}
	if _, err := s.Write(ctx, fmt.Sprintf("bfc handoff %s %s", v.ShortID, task), files); err != nil {
		return err
	}
	stored, err := core.ParseHandoff(h.ID, data)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(toHandoffJSON(v, stored, e.ID.String()))
	}
	out := cmd.OutOrStdout()
	report(out, string(e.Type), fmt.Sprintf("%s of %s with Handoff %s", task, v.ShortID, h.ID))
	_, err = fmt.Fprintln(out, "\nNext: bfc push shares it, so the next one sees the note.")
	return err
}

// planHandoffs returns the Handoffs of a plan in ULID order, and for each
// the ULID of the valid released or completed Event that names it ("" for
// a Handoff without its Event, which changes nothing). A file that cannot
// be read as a Handoff is skipped; bfc doctor reports it.
func planHandoffs(ctx context.Context, s *store.Store, v index.Plan) ([]core.Handoff, map[ulid.ULID]string, error) {
	hs, err := readHandoffs(ctx, s, v.ID)
	if err != nil {
		return nil, nil, err
	}
	events, err := readEvents(ctx, s, v.ID)
	if err != nil {
		return nil, nil, err
	}
	blob, err := s.ObjectID(ctx, core.PlanDir(v.ID)+"/plan.md")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, nil, err
	}
	byHandoff := map[ulid.ULID]string{}
	for h, e := range core.HandoffEvents(v.ID, events, blob) {
		byHandoff[h] = e.String()
	}
	return hs, byHandoff, nil
}

// readHandoffs returns the Handoffs of a plan in ULID order. A file that
// cannot be read as a Handoff is skipped; bfc doctor reports it.
func readHandoffs(ctx context.Context, s *store.Store, plan ulid.ULID) ([]core.Handoff, error) {
	files, err := readPlanFiles(ctx, s, plan, "handoff", ".md")
	if err != nil {
		return nil, err
	}
	var hs []core.Handoff
	for _, f := range files {
		if h, err := core.ParseHandoff(f.id, f.data); err == nil {
			hs = append(hs, h)
		}
	}
	return hs, nil
}

// readEvents returns the Events of a plan that parse, in ULID order.
func readEvents(ctx context.Context, s *store.Store, plan ulid.ULID) ([]core.Event, error) {
	files, err := readPlanFiles(ctx, s, plan, "events", ".yaml")
	if err != nil {
		return nil, err
	}
	var es []core.Event
	for _, f := range files {
		if e, err := core.ParseEvent(f.id, f.data); err == nil {
			es = append(es, e)
		}
	}
	return es, nil
}

// handoffJSON is the --json shape of a Handoff, documented in
// docs/design.md. Event is the ULID of its released or completed Event,
// "" for a Handoff without one.
type handoffJSON struct {
	ID      string      `json:"id"`
	Plan    string      `json:"plan"`
	ShortID string      `json:"short_id"`
	Task    string      `json:"task"`
	Author  core.Person `json:"author"`
	At      string      `json:"at"`
	Agent   string      `json:"agent,omitempty"`
	Done    bool        `json:"done"`
	Note    string      `json:"note"`
	Event   string      `json:"event"`
}

func toHandoffJSON(v index.Plan, h core.Handoff, event string) handoffJSON {
	return handoffJSON{
		ID: h.ID.String(), Plan: v.ID.String(), ShortID: v.ShortID, Task: h.Task, Author: h.Author,
		At: h.At.UTC().Format(time.RFC3339), Agent: h.Agent, Done: h.Done, Note: h.Note, Event: event,
	}
}

// writeTasks prints the Tasks section of bfc show: each Task of plan.md
// with its state and holder, and under it its Handoffs. Handoffs without
// their Event, or of a Task plan.md no longer has, follow as notes.
func writeTasks(b *strings.Builder, v index.Plan, hs []core.Handoff, events map[ulid.ULID]string) {
	views := core.TaskViews(v.Status, v.Plan)
	if len(views) == 0 && len(hs) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, t := range views {
		listed[t.ID] = true
	}
	notes := map[string]string{}
	for _, h := range hs {
		notes[h.ID.String()] = h.Note
	}
	b.WriteString("\n--- Tasks ---\n")
	for _, t := range views {
		holder := "-"
		if t.State != core.TaskOpen {
			holder = throughAgent(orDash(t.Holder.Name), t.Agent)
		}
		line := fmt.Sprintf("%s  %s  %s  %s", t.ID, t.State, holder, firstLine(t.Text))
		if note, ok := notes[t.Handoff]; ok {
			line += "  · " + firstLine(note)
		}
		b.WriteString(line + "\n")
		for _, h := range hs {
			if h.Task == t.ID && events[h.ID] != "" {
				writeHandoff(b, h, "")
			}
		}
	}
	for _, h := range hs {
		switch {
		case events[h.ID] == "":
			writeHandoff(b, h, "note without its Event, changes nothing: ")
		case !listed[h.Task]:
			writeHandoff(b, h, "of a Task plan.md no longer has: ")
		}
	}
}

func writeHandoff(b *strings.Builder, h core.Handoff, prefix string) {
	what := "released"
	if h.Done {
		what = "completed"
	}
	fmt.Fprintf(b, "  Handoff %s  %s%s %s by %s:\n", h.ID, prefix, h.Task, what, throughAgent(orDash(h.Author.Name), h.Agent))
	for _, line := range strings.Split(h.Note, "\n") {
		b.WriteString(strings.TrimRight("    "+line, " ") + "\n")
	}
}
