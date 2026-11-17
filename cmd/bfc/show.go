package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Print a plan: header, intent.md and plan.md",
		Long: `Print a plan: a header with its IDs, state, author and approvals, then
intent.md and, if it exists, plan.md. The ID is a Short ID (bfc-5v7x), a
prefix of at least four characters, or the full ULID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			views, err := loadPlans(ctx, s)
			if err != nil {
				return err
			}
			refs := make([]core.PlanRef, len(views))
			for i, v := range views {
				refs[i] = core.PlanRef{ID: v.ID, ShortID: v.Status.ShortID}
			}
			id, err := core.ResolveID(args[0], refs)
			var amb *core.AmbiguousIDError
			switch {
			case errors.As(err, &amb):
				return ambiguousError(amb, views)
			case errors.Is(err, core.ErrUnknownID):
				return fmt.Errorf("no plan %q; bfc list shows the plans", args[0])
			case err != nil:
				return err
			}
			for _, v := range views {
				if v.ID == id {
					return printPlan(cmd.OutOrStdout(), v)
				}
			}
			return nil
		},
	}
}

// ambiguousError lists the candidates with their titles.
func ambiguousError(amb *core.AmbiguousIDError, views []planView) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches several plans; give a longer ID:", amb.Query)
	for _, c := range amb.Candidates {
		for _, v := range views {
			if v.ID == c.ID {
				fmt.Fprintf(&b, "\n  %s  %s  %s", core.ShortID(c.ID, otherIDs(views)), c.ID, v.Title)
			}
		}
	}
	return errors.New(b.String())
}

// otherIDs returns the IDs of all plans, so that ShortID gives the shortest
// prefix that tells a candidate apart.
func otherIDs(views []planView) []ulid.ULID {
	ids := make([]ulid.ULID, len(views))
	for i, v := range views {
		ids[i] = v.ID
	}
	return ids
}

func printPlan(w io.Writer, v planView) error {
	approvals := "-"
	if v.Status.RequiredApprovals > 0 {
		approvals = fmt.Sprintf("%d/%d", len(v.Status.Approvers), v.Status.RequiredApprovals)
		if len(v.Status.Approvers) > 0 {
			approvals += " (" + strings.Join(v.Status.Approvers, ", ") + ")"
		}
	}
	author := "-"
	if v.Status.Author.Email != "" {
		author = fmt.Sprintf("%s <%s>", v.Status.Author.Name, v.Status.Author.Email)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", v.Status.ShortID, v.Title)
	fmt.Fprintf(&b, "ID:         %s\n", v.ID)
	fmt.Fprintf(&b, "State:      %s\n", v.Status.State)
	fmt.Fprintf(&b, "Author:     %s\n", author)
	fmt.Fprintf(&b, "Approvals:  %s\n", approvals)
	fmt.Fprintf(&b, "Activity:   %s\n", formatTime(v.LastActivity))
	if len(v.Labels) > 0 {
		fmt.Fprintf(&b, "Labels:     %s\n", strings.Join(v.Labels, ", "))
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{"intent.md", v.Intent}, {"plan.md", v.Plan}} {
		if f.data == nil {
			continue
		}
		fmt.Fprintf(&b, "\n--- %s ---\n%s", f.name, f.data)
		if !strings.HasSuffix(string(f.data), "\n") {
			b.WriteString("\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
