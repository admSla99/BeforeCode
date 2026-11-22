package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newClaimCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "claim [<plan-id>] [T-n]",
		Short: "Take a Task of an approved plan",
		Long: `Take a Task of an approved or in-progress plan and become its holder
(ADR 0013): the given Task, or the first open Task of the plan; without a
plan ID, of the Current plan. Agents and people may claim.

bfc claim pulls first, writes a claimed Event, pushes, and derives the plan
again. When another Claim on the same Task came first, that one wins in every
clone: bfc claim then says who holds the Task and fails. In local mode it
does not sync.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, task, err := claimArgs(args)
			if err != nil {
				return err
			}
			return runClaim(cmd, query, task, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the Claim as JSON for scripts and agents")
	return cmd
}

// claimArgs splits the arguments of bfc claim into a plan ID ("" for the
// Current plan) and a Task ID ("" for the next open one).
func claimArgs(args []string) (query, task string, err error) {
	isTask := func(a string) bool { return strings.HasPrefix(strings.ToUpper(a), "T-") }
	switch len(args) {
	case 1:
		if isTask(args[0]) {
			return "", strings.ToUpper(args[0]), nil
		}
		return args[0], "", nil
	case 2:
		if !isTask(args[1]) {
			return "", "", fmt.Errorf("%q is not a Task ID such as T-1", args[1])
		}
		return args[0], strings.ToUpper(args[1]), nil
	}
	return "", "", nil
}

func runClaim(cmd *cobra.Command, query, task string, asJSON bool) error {
	ctx := cmd.Context()
	s, err := openPlans(cmd, forWriting)
	if err != nil {
		return err
	}
	set, err := syncSettings(s)
	if err != nil {
		return err
	}
	if !set.local {
		if _, err := s.Pull(ctx, set.remote, store.KeepNeither); err != nil {
			return syncError(cmd, s, err)
		}
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
	if task == "" {
		next, ok := core.NextTask(v.Status, v.Plan)
		if err := core.CheckHasWork(v.Status); err != nil {
			return err
		}
		switch {
		case !ok:
			return fmt.Errorf("%s has no open Task; bfc ready lists the open Tasks of the approved plans", v.ShortID)
		}
		task = next.ID
	}
	if err := core.CheckClaim(v.Status, v.Plan, task); err != nil {
		return err
	}
	actor, err := currentUser(ctx, s)
	if err != nil {
		return err
	}
	now := time.Now()
	e := core.Event{
		ID: ulid.MustNew(ulid.Timestamp(now), rand.Reader), Type: core.Claimed, Plan: v.ID,
		Author: actor, At: now, Agent: insideAgent(cmd), Task: task,
	}
	path := core.PlanDir(v.ID) + "/events/" + e.ID.String() + ".yaml"
	if _, err := s.Write(ctx, "bfc claim "+v.ShortID+" "+task, map[string][]byte{path: core.EncodeEvent(e)}); err != nil {
		return err
	}
	if !set.local {
		if _, err := s.Push(ctx, set.remote); err != nil {
			return fmt.Errorf("%s of %s is claimed in this clone but not shared; run bfc push to share it: %w", task, v.ShortID, syncError(cmd, s, err))
		}
	}
	after, err := loadPlan(ctx, s, v.ID)
	if err != nil {
		return err
	}
	if err := claimOutcome(after.Status, task, e.ID); err != nil {
		return err
	}
	return printClaim(cmd.OutOrStdout(), after, task, asJSON)
}

// claimOutcome reports whether the Claim with ID ours holds the Task after
// the sync: when an earlier Claim reached the plan, that one wins.
func claimOutcome(st core.Status, task string, ours ulid.ULID) error {
	c, ok := st.Claims[task]
	switch {
	case !ok:
		return fmt.Errorf("the Claim of %s of %s does not count; run bfc doctor", task, st.ShortID)
	case c.Event != ours:
		return fmt.Errorf("%s of %s went to %s, whose Claim came first; bfc ready lists the open Tasks", task, st.ShortID, c.Holder.Email)
	}
	return nil
}

// claimJSON is the --json shape of bfc claim, documented in docs/design.md.
type claimJSON struct {
	Plan    string      `json:"plan"`
	ShortID string      `json:"short_id"`
	Task    string      `json:"task"`
	Text    string      `json:"text"`
	Holder  core.Person `json:"holder"`
	Agent   string      `json:"agent,omitempty"`
	Event   string      `json:"event"`
	State   core.State  `json:"state"`
}

func printClaim(w io.Writer, v index.Plan, task string, asJSON bool) error {
	var text string
	for _, t := range core.TaskViews(v.Status, v.Plan) {
		if t.ID == task {
			text = t.Text
		}
	}
	c := v.Status.Claims[task]
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(claimJSON{
			Plan: v.ID.String(), ShortID: v.ShortID, Task: task, Text: text, Holder: c.Holder, Agent: c.Agent,
			Event: c.Event.String(), State: v.Status.State,
		})
	}
	report(w, "claimed", fmt.Sprintf("%s of %s: %s", task, v.ShortID, firstLine(text)))
	return nil
}

func newReadyCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ready [id]",
		Short: "List the open Tasks of the approved plans",
		Long: `List the Tasks nobody holds of the approved and in-progress plans, each
plan's Tasks in the order of its plan.md, which is the order of the work.
With an ID, only that plan's. bfc claim takes one.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			var views []index.Plan
			if len(args) == 1 {
				v, err := resolvePlan(cmd.Context(), s, args[0])
				if err != nil {
					return err
				}
				views = []index.Plan{v}
			} else if views, err = loadPlansWithDocuments(cmd.Context(), s); err != nil {
				return err
			}
			return printReady(cmd.OutOrStdout(), views, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON for scripts and agents")
	return cmd
}

// readyJSON is an open Task in bfc ready --json, documented in
// docs/design.md.
type readyJSON struct {
	Plan    string `json:"plan"`
	ShortID string `json:"short_id"`
	Title   string `json:"title"`
	Task    string `json:"task"`
	Text    string `json:"text"`
}

func printReady(w io.Writer, views []index.Plan, asJSON bool) error {
	out := []readyJSON{}
	for _, v := range views {
		if !core.HasWork(v.Status.State) {
			continue
		}
		for _, t := range core.TaskViews(v.Status, v.Plan) {
			if t.State == core.TaskOpen {
				out = append(out, readyJSON{Plan: v.ID.String(), ShortID: v.ShortID, Title: v.Title, Task: t.ID, Text: t.Text})
			}
		}
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	if len(out) == 0 {
		_, err := fmt.Fprintln(w, "No open Tasks in approved plans.")
		return err
	}
	var b strings.Builder
	for _, r := range out {
		fmt.Fprintf(&b, "%s  %s  %s  (%s)\n", r.ShortID, r.Task, firstLine(r.Text), r.Title)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
