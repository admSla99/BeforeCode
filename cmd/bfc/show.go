package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
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
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			v, err := resolvePlan(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}
			return printPlan(cmd.OutOrStdout(), v)
		},
	}
}

// resolvePlan loads the plans and returns the one the query names (see
// core.ResolveID), with errors that list candidates or point to bfc list.
func resolvePlan(ctx context.Context, s *store.Store, query string) (index.Plan, error) {
	views, err := loadPlans(ctx, s)
	if err != nil {
		return index.Plan{}, err
	}
	byID := make(map[ulid.ULID]index.Plan, len(views))
	refs := make([]core.PlanHandle, len(views))
	for i, v := range views {
		byID[v.ID] = v
		refs[i] = core.PlanHandle{ID: v.ID, ShortID: v.Status.ShortID}
	}
	id, err := core.ResolveID(query, refs)
	var amb *core.AmbiguousIDError
	switch {
	case errors.As(err, &amb):
		return index.Plan{}, ambiguousError(amb, byID)
	case errors.Is(err, core.ErrUnknownID):
		return index.Plan{}, fmt.Errorf("no plan %q; bfc list shows the plans", query)
	case err != nil:
		return index.Plan{}, err
	}
	return byID[id], nil
}

// ambiguousError lists the candidates with their Short IDs, ULIDs and
// titles. The ULID always tells them apart.
func ambiguousError(amb *core.AmbiguousIDError, byID map[ulid.ULID]index.Plan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches several plans; give a longer ID or the ULID:", amb.Query)
	for _, c := range amb.Candidates {
		v := byID[c.ID]
		fmt.Fprintf(&b, "\n  %s  %s  %s", v.ShortID, c.ID, v.Title)
	}
	return errors.New(b.String())
}

func printPlan(w io.Writer, v index.Plan) error {
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
	fmt.Fprintf(&b, "%s  %s\n", v.ShortID, v.Title)
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
