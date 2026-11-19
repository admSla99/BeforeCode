package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/index"
	"github.com/admSla99/BeforeCode/internal/store"
)

// currentPlanError is a Current plan that cannot be used, with what to do
// about it, as opposed to a failure to read the repository.
type currentPlanError string

func (e currentPlanError) Error() string { return string(e) }

// errNoCurrentPlan says how to get a Current plan.
const errNoCurrentPlan = currentPlanError("there is no Current plan: run bfc start <id> to work on a plan in its own branch, or bfc use <id> to set it without changing the branch")

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
			w := cmd.OutOrStdout()
			branch, err := s.Branch(cmd.Context())
			if err != nil {
				return err
			}
			if short, ok := core.ShortIDInBranch(branch); ok {
				if id, _ := branchPlan(cmd.Context(), s, branch); id != v.ID {
					_, err = fmt.Fprintf(w, "%s  %s is set with bfc use, but the branch %s names %s, which stays the Current plan\n", v.ShortID, v.Title, branch, short)
					return err
				}
			}
			_, err = fmt.Fprintf(w, "%s  %s is the Current plan\n", v.ShortID, v.Title)
			return err
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "remove the plan set with bfc use")
	return cmd
}

// clearUsedPlan removes the plan set with bfc use. It needs no plans ref.
func clearUsedPlan(cmd *cobra.Command) error {
	s, err := openStore(cmd)
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
		return ulid.ULID{}, currentPlanError(fmt.Sprintf("%s does not hold a plan ULID; run bfc use <id> or bfc use --clear", path))
	}
	return id, nil
}

// currentPlan returns the Current plan (ADR 0009), with its documents. It
// returns errNoCurrentPlan when there is none, and a currentPlanError that
// says what to do when the branch or bfc use names a plan that does not
// exist.
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
		byID, refs := planHandles(views)
		cur, err := core.CurrentPlan(branch, used, refs)
		var be *core.BranchPlanError
		var amb *core.AmbiguousIDError
		// An ambiguous Short ID in the branch is also a BranchPlanError, so
		// it is told apart first.
		switch {
		case errors.As(err, &amb) && errors.As(err, &be):
			return currentPlanError(fmt.Sprintf("the branch %s names %s, which matches several plans; rename the branch with a longer ID:%s", be.Branch, be.ShortID, candidates(amb, byID)))
		case errors.As(err, &be):
			return currentPlanError(fmt.Sprintf("the branch %s names %s, but this clone has no such plan: run bfc pull if someone else created it; otherwise the branch name is wrong (bfc list shows the plans)", be.Branch, be.ShortID))
		case errors.Is(err, core.ErrUnknownID):
			return currentPlanError(fmt.Sprintf("the plan %s set with bfc use does not exist here; run bfc pull, or bfc use --clear", used))
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
		Long: `Work on a plan on its branch, so that everyone works on the same one. bfc
start first fetches the branches of the sync remote, then switches to the
local branch that carries the plan's Short ID, else takes the plan's branch
from the remote and tracks it, else creates <Short ID>-<slug of the title>
from the current HEAD. The Short ID in the branch name makes the plan the
Current plan. Uncommitted changes come along as with git switch; if git
refuses, nothing changes. A closed plan cannot be started. bfc start never
pushes and writes nothing to the plans ref.`,
		Args: cobra.ExactArgs(1),
		RunE: runStart,
	}
}

// fetchTimeout bounds the fetch of bfc start.
const fetchTimeout = 30 * time.Second

func runStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	s, err := openPlans(cmd, forReading)
	if err != nil {
		return err
	}
	// In local mode there is no remote to share branches with.
	remote, err := syncRemote(s, "", false)
	if err != nil {
		remote = ""
	}
	v, err := resolvePlan(ctx, s, args[0])
	if errors.Is(err, core.ErrUnknownID) && remote != "" {
		return unknownHere(ctx, s, remote, args[0], err)
	}
	if err != nil {
		return err
	}
	if v.Status.State == core.StateClosed {
		return fmt.Errorf("%s is closed and cannot be started", v.ShortID)
	}
	if remote != "" {
		fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
		err := s.FetchBranches(fctx, remote)
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not fetch the branches of %s, so a branch of this plan pushed by someone else may be missing: %v\n", remote, err)
		}
	}
	b, err := planBranch(ctx, s, v, remote)
	if err != nil {
		return err
	}
	current, err := s.Branch(ctx)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	plan := fmt.Sprintf("%s  %s is the Current plan", v.ShortID, v.Title)
	switch {
	case b.name == current:
		_, err = fmt.Fprintf(w, "already on %s; %s\n", b.name, plan)
	case b.kind == localBranch:
		if err := s.Switch(ctx, b.name); err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "switched to %s; %s\n", b.name, plan)
	case b.kind == remoteBranch:
		if err := s.SwitchTracking(ctx, remote, b.name); err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "switched to %s, tracking %s/%s; %s\n", b.name, remote, b.name, plan)
		return err
	default:
		if err := s.SwitchNew(ctx, b.name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "created and switched to %s; %s\n", b.name, plan); err != nil {
			return err
		}
		if remote != "" {
			_, err = fmt.Fprintf(w, "push it with git push -u %s %s, so that others who run bfc start %s work on it too\n", remote, b.name, v.ShortID)
		}
		return err
	}
	if err != nil {
		return err
	}
	return reportBehind(ctx, s, w, b.name)
}

// unknownHere explains a plan ID this clone does not know by asking the
// remote's plans ref: a plan the remote has needs bfc pull. When the remote
// cannot be asked, it returns err with that said.
func unknownHere(ctx context.Context, s *store.Store, remote, query string, err error) error {
	fctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	has, ferr := remoteHasPlan(fctx, s, remote, query)
	switch {
	case ferr != nil:
		return fmt.Errorf("%w (could not ask %s: %v)", err, remote, ferr)
	case has:
		return fmt.Errorf("%s has %s, but this clone does not yet: run bfc pull, then bfc start %s", remote, query, query)
	default:
		return fmt.Errorf("no plan %q, neither in this clone nor on %s; bfc list shows the plans", query, remote)
	}
}

// remoteHasPlan fetches the remote's plans ref and reports whether a plan
// there matches the query. It does not change the local plans ref.
func remoteHasPlan(ctx context.Context, s *store.Store, remote, query string) (bool, error) {
	commit, err := s.Fetch(ctx, remote)
	if err != nil || commit == "" {
		return false, err
	}
	entries, err := s.Entries(ctx, commit, core.PlansDir)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Without their events the Short IDs are unknown, but a Short ID is a
	// prefix of the ULID's random part, which ResolveID matches.
	var refs []core.PlanHandle
	for _, e := range entries {
		if id, err := ulid.ParseStrict(e.Name); e.Dir && err == nil {
			refs = append(refs, core.PlanHandle{ID: id})
		}
	}
	_, err = core.ResolveID(query, refs)
	var amb *core.AmbiguousIDError
	return err == nil || errors.As(err, &amb), nil
}

// reportBehind says when the branch lacks commits of its upstream.
func reportBehind(ctx context.Context, s *store.Store, w io.Writer, branch string) error {
	upstream, n, err := s.Behind(ctx, branch)
	if err != nil || n == 0 {
		return err
	}
	commits := "commits"
	if n == 1 {
		commits = "commit"
	}
	_, err = fmt.Fprintf(w, "%s is %d %s behind %s; run git pull\n", branch, n, commits, upstream)
	return err
}

// branchKind says where planBranch found the plan's branch.
type branchKind int

const (
	newBranch branchKind = iota
	localBranch
	remoteBranch
)

type planBranchRef struct {
	name string
	kind branchKind
}

// planBranch finds the plan's branch: a local branch whose name makes the
// plan the Current plan (core.CurrentPlan), else such a branch of the
// remote, else the name of a new one. Several matches at the same place are
// an error that lists them.
func planBranch(ctx context.Context, s *store.Store, v index.Plan, remote string) (planBranchRef, error) {
	refs, err := planRefs(ctx, s)
	if err != nil {
		return planBranchRef{}, err
	}
	local, err := s.LocalBranches(ctx)
	if err != nil {
		return planBranchRef{}, err
	}
	var theirs []string
	if remote != "" {
		if theirs, err = s.RemoteBranches(ctx, remote); err != nil {
			return planBranchRef{}, err
		}
	}
	for _, place := range []struct {
		kind     branchKind
		prefix   string
		branches []string
	}{{localBranch, "", local}, {remoteBranch, remote + "/", theirs}} {
		var found []string
		for _, b := range place.branches {
			if cur, err := core.CurrentPlan(b, ulid.ULID{}, refs); err == nil && cur.ID == v.ID {
				found = append(found, b)
			}
		}
		switch len(found) {
		case 0:
			continue
		case 1:
			return planBranchRef{name: found[0], kind: place.kind}, nil
		default:
			return planBranchRef{}, fmt.Errorf("several branches carry %s; switch to one with git switch:\n  %s%s", v.ShortID, place.prefix, strings.Join(found, "\n  "+place.prefix))
		}
	}
	return planBranchRef{name: core.BranchName(v.ShortID, v.Title), kind: newBranch}, nil
}

// branchPlan returns the plan the branch name makes the Current plan, or
// the zero ULID when it names none, or names it badly.
func branchPlan(ctx context.Context, s *store.Store, branch string) (ulid.ULID, error) {
	refs, err := planRefs(ctx, s)
	if err != nil {
		return ulid.ULID{}, err
	}
	cur, err := core.CurrentPlan(branch, ulid.ULID{}, refs)
	if err != nil {
		return ulid.ULID{}, nil
	}
	return cur.ID, nil
}

// planRefs returns the handles of every plan, from the index.
func planRefs(ctx context.Context, s *store.Store) ([]core.PlanHandle, error) {
	views, err := loadPlans(ctx, s)
	_, refs := planHandles(views)
	return refs, err
}
