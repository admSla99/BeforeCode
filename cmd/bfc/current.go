package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// errNoCurrentPlan says how to get a Current plan.
var errNoCurrentPlan = errors.New("there is no Current plan: run bfc start <id> to work on a plan in its own branch, or bfc use <id> to set it without changing the branch")

func newUseCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "use <id> | --clear",
		Short: "Set the Current plan without changing the branch",
		Long: `Set the Current plan without changing the branch, for work that cannot be
on a branch named after the plan, such as a detached HEAD. The plan's ULID is
kept in .git/beforecode/current of this clone. A Short ID in the branch name
still wins over it (bfc start). --clear removes it.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if clear {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear {
				return clearUsedPlan(cmd)
			}
			s, err := openPlans(cmd, forReading)
			if err != nil {
				return err
			}
			v, err := resolvePlan(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}
			path, err := usedPlanFile(cmd.Context(), s)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(v.ID.String()+"\n"), 0o644); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s is the Current plan\n", v.ShortID, v.Title)
			return err
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the plan set with bfc use")
	return cmd
}

// clearUsedPlan removes the plan set with bfc use. It needs no plans ref.
func clearUsedPlan(cmd *cobra.Command) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	s, err := store.Open(wd)
	if err != nil {
		return err
	}
	path, err := usedPlanFile(cmd.Context(), s)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), "no plan is set with bfc use")
	return err
}

// usedPlanFile is .git/beforecode/current of this working tree.
func usedPlanFile(ctx context.Context, s *store.Store) (string, error) {
	dir, err := s.GitDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "beforecode", "current"), nil
}

// usedPlan returns the plan set with bfc use, or the zero ULID.
func usedPlan(ctx context.Context, s *store.Store) (ulid.ULID, error) {
	path, err := usedPlanFile(ctx, s)
	if err != nil {
		return ulid.ULID{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ulid.ULID{}, nil
	}
	if err != nil {
		return ulid.ULID{}, err
	}
	id, err := ulid.ParseStrict(strings.TrimSpace(string(data)))
	if err != nil {
		return ulid.ULID{}, fmt.Errorf("%s does not hold a plan ULID; run bfc use <id> or bfc use --clear", path)
	}
	return id, nil
}

// currentPlan returns the Current plan (ADR 0009), with its documents. It
// returns errNoCurrentPlan when there is none, and says what to do when the
// branch or bfc use names a plan that does not exist.
func currentPlan(ctx context.Context, s *store.Store) (index.Plan, error) {
	branch, err := s.Branch(ctx)
	if err != nil {
		return index.Plan{}, err
	}
	used, err := usedPlan(ctx, s)
	if err != nil {
		return index.Plan{}, err
	}
	var p index.Plan
	err = withIndex(ctx, s, func(ix *index.Index) error {
		views, err := ix.Plans(ctx)
		if err != nil {
			return err
		}
		byID := make(map[ulid.ULID]index.Plan, len(views))
		refs := make([]core.PlanHandle, len(views))
		for i, v := range views {
			byID[v.ID] = v
			refs[i] = core.PlanHandle{ID: v.ID, ShortID: v.Status.ShortID}
		}
		cur, err := core.CurrentPlan(branch, used, refs)
		var be *core.BranchPlanError
		var amb *core.AmbiguousIDError
		switch {
		case errors.As(err, &amb) && errors.As(err, &be):
			return fmt.Errorf("the branch %s names %s, which matches several plans; rename the branch with a longer ID:%s", be.Branch, be.ShortID, candidates(amb, byID))
		case errors.As(err, &be):
			return fmt.Errorf("the branch %s names %s, which is no plan; bfc list shows the plans", be.Branch, be.ShortID)
		case errors.Is(err, core.ErrUnknownID):
			return fmt.Errorf("the plan %s set with bfc use does not exist here; run bfc pull, or bfc use --clear", used)
		case err != nil:
			return err
		case cur.Source == core.NoCurrent:
			return errNoCurrentPlan
		}
		p, err = ix.Plan(ctx, cur.ID)
		return err
	})
	return p, err
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <id>",
		Short: "Work on a plan: switch to its branch, which makes it the Current plan",
		Long: `Work on a plan: create the branch <Short ID>-<slug of the title> from the
current HEAD and switch to it, or switch to the local branch that already
carries the plan's Short ID. The Short ID in the branch name makes the plan the
Current plan. Uncommitted changes come along as with git switch; if git
refuses, nothing changes. A closed plan cannot be started. bfc start writes
nothing to the plans ref.`,
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
			if v.Status.State == core.StateClosed {
				return fmt.Errorf("%s is closed and cannot be started", v.ShortID)
			}
			branch, create, err := planBranch(ctx, s, v.ShortID, v.Title)
			if err != nil {
				return err
			}
			current, err := s.Branch(ctx)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if branch == current {
				_, err = fmt.Fprintf(w, "already on %s; %s  %s is the Current plan\n", branch, v.ShortID, v.Title)
				return err
			}
			if err := s.Switch(ctx, branch, create); err != nil {
				return err
			}
			verb := "switched to"
			if create {
				verb = "created and switched to"
			}
			_, err = fmt.Fprintf(w, "%s %s; %s  %s is the Current plan\n", verb, branch, v.ShortID, v.Title)
			return err
		},
	}
}

// planBranch returns the local branch that carries the plan's Short ID, or
// the name of a new one with create set. Several such branches are an
// error that lists them.
func planBranch(ctx context.Context, s *store.Store, shortID, title string) (branch string, create bool, err error) {
	branches, err := s.LocalBranches(ctx)
	if err != nil {
		return "", false, err
	}
	var found []string
	for _, b := range branches {
		if id, ok := core.ShortIDInBranch(b); ok && strings.EqualFold(id, shortID) {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 0:
		return core.BranchName(shortID, title), true, nil
	case 1:
		return found[0], false, nil
	default:
		return "", false, fmt.Errorf("several branches carry %s; switch to one with git switch:\n  %s", shortID, strings.Join(found, "\n  "))
	}
}
