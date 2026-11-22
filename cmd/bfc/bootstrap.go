package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

func newBootstrapCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Get the plans of a repository that already uses BeforeCode",
		Long: `Join a repository that already uses BeforeCode: check that the remote has
the plans ref, add its fetch refspec, fetch it and create the local plans ref.
Your working tree, index and branch are not touched.

In a clone that is already set up, bootstrap pulls like bfc pull.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, remote, _, err := prepareSync(cmd, f)
			if err != nil {
				return err
			}
			return runBootstrap(cmd, s, remote)
		},
	}
	f.addRemote(cmd)
	return cmd
}

func runBootstrap(cmd *cobra.Command, s *store.Store, remote string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	initialised, err := s.Initialised(ctx)
	if err != nil {
		return err
	}
	has, err := s.RemoteHasPlans(ctx, remote)
	if err != nil {
		return err
	}
	if has {
		// The remote may lose its plans ref between the two calls.
		tip, err := s.Fetch(ctx, remote)
		if err != nil {
			return err
		}
		has = tip != ""
	}
	switch {
	case !has && initialised:
		return fmt.Errorf("%s has no plans yet, but this clone has: run bfc push to share them", remote)
	case !has:
		return fmt.Errorf("%s has no plans in %s: run bfc init to start using BeforeCode in this repository", remote, store.Ref)
	}
	report(out, "checked", remote+" has plans in "+store.Ref)

	if err := checkRemoteFormat(ctx, out, s, remote); err != nil {
		return err
	}
	added, err := s.AddFetchRefspec(ctx, remote)
	if err != nil {
		return err
	}
	what := "fetch refspec " + store.FetchRefspec + " for " + remote
	if added {
		report(out, "added", what)
	} else {
		report(out, "kept", what)
	}

	if initialised {
		report(out, "kept", store.Ref+": this repository was already set up")
	}
	r, err := s.Integrate(ctx, remote, store.KeepNeither)
	if err != nil {
		return syncError(cmd, s, err)
	}
	if initialised {
		reportPull(out, remote, r)
	} else {
		report(out, "created", store.Ref+" ("+short(r.Tip)+")")
	}
	_, err = fmt.Fprintln(out, "\nNext: bfc list")
	return err
}

// checkRemoteFormat refuses a fetched plans ref whose format-version this
// binary cannot use (ADR 0002 section 2).
func checkRemoteFormat(ctx context.Context, out io.Writer, s *store.Store, remote string) error {
	data, err := s.ReadRemote(ctx, "format-version")
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("the plans of %s have no format-version file; ask whoever set them up to run bfc doctor", remote)
	}
	if err != nil {
		return err
	}
	v, err := core.ParseFormatVersion(data)
	switch {
	case err != nil:
		return fmt.Errorf("the plans of %s have an unreadable %w", remote, err)
	case v > core.FormatVersion:
		return fmt.Errorf("the plans of %s have format-version %d, newer than this bfc supports (%d); update bfc", remote, v, core.FormatVersion)
	case v < core.FormatVersion:
		report(out, "note", fmt.Sprintf("the plans of %s have format-version %d: this bfc reads them, and writes once someone runs bfc migrate", remote, v))
	}
	return nil
}
