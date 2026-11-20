package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
)

// listedStates are the states --state accepts, in lifecycle order.
var listedStates = []core.State{
	core.StateDraft, core.StateInReview, core.StateApproved, core.StateInProgress,
	core.StateDone, core.StateClosed, core.StateInvalid,
}

// timeLayout is how list and show print times, in local time.
const timeLayout = "2006-01-02 15:04"

func newListCmd() *cobra.Command {
	var state string
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List plans, newest activity first",
		Long: `List plans, one per line: Short ID, state, title, author and last activity,
newest activity first. Last activity is the time of the newest change to any
file of the plan. Closed plans are left out unless asked for with --state
closed or --all.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if state != "" && !slices.Contains(listedStates, core.State(state)) {
				return fmt.Errorf("unknown state %q; use one of %s", state, joinStates(listedStates))
			}
			if all && state != "" {
				return fmt.Errorf("--all and --state cannot be used together")
			}
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			views, err := loadPlans(cmd.Context(), s)
			if err != nil {
				return err
			}
			switch {
			case state != "":
				views = slices.DeleteFunc(views, func(v index.Plan) bool { return v.Status.State != core.State(state) })
			case !all:
				views = slices.DeleteFunc(views, func(v index.Plan) bool { return v.Status.State == core.StateClosed })
			}
			if asJSON {
				return printJSON(cmd, views)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, v := range views {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.ShortID, v.Status.State, v.Title, orDash(v.Status.Author.Email), formatTime(v.LastActivity))
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "show only plans in this state ("+joinStates(listedStates)+")")
	cmd.Flags().BoolVar(&all, "all", false, "also show closed plans")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON for scripts and agents")
	return cmd
}

// planJSON is the --json shape, documented in docs/design.md.
type planJSON struct {
	ID                string      `json:"id"`
	ShortID           string      `json:"short_id"`
	State             core.State  `json:"state"`
	Title             string      `json:"title"`
	Labels            []string    `json:"labels"`
	Author            core.Person `json:"author"`
	LastActivity      string      `json:"last_activity"`
	Approvals         int         `json:"approvals"`
	RequiredApprovals int         `json:"required_approvals"`
}

func printJSON(cmd *cobra.Command, views []index.Plan) error {
	out := make([]planJSON, 0, len(views))
	for _, v := range views {
		out = append(out, toPlanJSON(v))
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// toPlanJSON is the plan in the shape of bfc list --json.
func toPlanJSON(v index.Plan) planJSON {
	return planJSON{
		ID: v.ID.String(), ShortID: v.ShortID, State: v.Status.State,
		Title: v.Title, Labels: v.Labels, Author: v.Status.Author,
		LastActivity: v.LastActivity.UTC().Format(time.RFC3339),
		Approvals:    len(v.Status.Approvers), RequiredApprovals: v.Status.RequiredApprovals,
	}
}

func joinStates(states []core.State) string {
	names := make([]string, len(states))
	for i, s := range states {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(timeLayout)
}
