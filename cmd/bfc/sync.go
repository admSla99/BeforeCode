package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/admSla99/BeforeCode/internal/config"
	"github.com/admSla99/BeforeCode/internal/store"
)

// defaultRemote is used when the repository has no config file.
const defaultRemote = "origin"

// syncFlags are the flags push, pull and sync share.
type syncFlags struct {
	remote string
	keep   string
}

func (f *syncFlags) addRemote(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.remote, "remote", "", "git remote to sync with (default: sync.remote in "+config.Path+", or origin)")
}

func (f *syncFlags) addKeep(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.keep, "keep", "", "on a conflict, keep the local or the remote version of the conflicting files (local|remote)")
}

func newPushCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push the plans ref to the remote",
		Long: `Push refs/beforecode/plans to the remote, never by force. If someone else
pushed first, bfc pulls their changes, merges them and tries again.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, remote, _, err := prepareSync(cmd, f)
			if err != nil {
				return err
			}
			r, err := s.Push(cmd.Context(), remote)
			if err != nil {
				return syncError(cmd, s, err)
			}
			reportPush(cmd.OutOrStdout(), remote, r)
			return nil
		},
	}
	f.addRemote(cmd)
	return cmd
}

func newPullCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fetch the plans ref from the remote and merge it into yours",
		Long: `Fetch refs/beforecode/plans from the remote and integrate it into the local
plans ref. Diverged histories are merged in memory with a merge commit; your
working tree, index and branch are not touched.

If both sides changed the same lines of a file, nothing is committed and both
versions are listed. Run pull again with --keep local or --keep remote to
choose one, then edit the plan to combine them if needed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, remote, keep, err := prepareSync(cmd, f)
			if err != nil {
				return err
			}
			r, err := s.Pull(cmd.Context(), remote, keep)
			if err != nil {
				return syncError(cmd, s, err)
			}
			reportPull(cmd.OutOrStdout(), remote, r)
			return nil
		},
	}
	f.addRemote(cmd)
	f.addKeep(cmd)
	return cmd
}

func newSyncCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull, then push the plans ref",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, remote, keep, err := prepareSync(cmd, f)
			if err != nil {
				return err
			}
			pulled, pushed, err := s.Sync(cmd.Context(), remote, keep)
			// Sync pushes only after a successful pull.
			if err == nil || pushed.Attempts > 0 {
				reportPull(cmd.OutOrStdout(), remote, pulled)
			}
			if err != nil {
				return syncError(cmd, s, err)
			}
			reportPush(cmd.OutOrStdout(), remote, pushed)
			return nil
		},
	}
	f.addRemote(cmd)
	f.addKeep(cmd)
	return cmd
}

// prepareSync opens the store and works out the remote and the keep side.
func prepareSync(cmd *cobra.Command, f syncFlags) (*store.Store, string, store.Keep, error) {
	var keep store.Keep
	switch f.keep {
	case "":
	case "local":
		keep = store.KeepLocal
	case "remote":
		keep = store.KeepRemote
	default:
		return nil, "", 0, fmt.Errorf("--keep must be local or remote, not %q", f.keep)
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", 0, err
	}
	s, err := store.Open(wd)
	if err != nil {
		return nil, "", 0, err
	}
	remote, err := syncRemote(s, f.remote, cmd.Flags().Changed("remote"))
	if err != nil {
		return nil, "", 0, err
	}
	ok, err := s.HasRemote(cmd.Context(), remote)
	if err != nil {
		return nil, "", 0, err
	}
	if !ok {
		return nil, "", 0, fmt.Errorf("git remote %q does not exist; add it with git remote add %s <url>, or name another with --remote", remote, remote)
	}
	return s, remote, keep, nil
}

// syncRemote returns the remote to sync with: the --remote flag, else
// sync.remote from the config file, else origin when there is no config
// file. In Local mode only an explicit --remote syncs.
func syncRemote(s *store.Store, flag string, flagSet bool) (string, error) {
	if flagSet {
		if flag == "" {
			return "", errors.New("--remote needs a remote name")
		}
		return flag, nil
	}
	set, err := syncSettings(s)
	if err != nil {
		return "", err
	}
	if set.local {
		return "", fmt.Errorf("sync is off in %s (local mode); plans stay in this clone. Use --remote to sync anyway", config.Path)
	}
	return set.remote, nil
}

// syncConfig is what the config file says about sync.
type syncConfig struct {
	remote   string        // the remote to sync with
	local    bool          // local mode: never sync
	interval time.Duration // between background syncs in the TUI
}

// syncSettings reads sync from the config file; without one it syncs with
// origin every DefaultSyncInterval.
func syncSettings(s *store.Store) (syncConfig, error) {
	cfg, found, err := readConfig(s)
	switch {
	case err != nil:
		return syncConfig{}, err
	case !found:
		return syncConfig{remote: defaultRemote, interval: config.DefaultSyncInterval}, nil
	}
	return syncConfig{remote: cfg.Remote, local: !cfg.SyncEnabled, interval: cfg.Interval()}, nil
}

// syncError explains a failed sync. A conflict lists both versions of each
// file and how to resolve it.
func syncError(cmd *cobra.Command, s *store.Store, err error) error {
	var ce *store.ConflictError
	switch {
	case errors.As(err, &ce):
		w := cmd.ErrOrStderr()
		_, _ = fmt.Fprintf(w, "Your plans and %s changed the same files differently. Nothing was committed.\n\n", ce.Remote)
		for _, c := range ce.Conflicts {
			_, _ = fmt.Fprintf(w, "conflict %s\n", c.Path)
			_, _ = fmt.Fprintf(w, "  local:  %s\n", versionCommand(ce.LocalCommit, c.Path, c.Local))
			_, _ = fmt.Fprintf(w, "  remote: %s\n", versionCommand(ce.RemoteCommit, c.Path, c.Remote))
		}
		_, _ = fmt.Fprintf(w, `
To resolve, keep one side for these files and merge everything else:
  bfc pull --keep local     keep your version
  bfc pull --keep remote    keep the version from %s
Then edit the plan (bfc edit) to bring in what you need from the other
version, and run bfc push.
`, ce.Remote)
		return fmt.Errorf("conflict with %s in %d file(s)", ce.Remote, len(ce.Conflicts))
	case errors.Is(err, store.ErrNotInitialised):
		return notInitialised(cmd, s)
	case errors.Is(err, store.ErrPushRetriesExhausted):
		return fmt.Errorf("others kept pushing plans while bfc tried %d times; run bfc push again", store.MaxPushAttempts)
	}
	return err
}

// versionCommand says how to see one side's version of a file.
func versionCommand(commit, path, blob string) string {
	if blob == "" {
		return "(deleted)"
	}
	return "git show " + commit[:12] + ":" + path
}

func reportPull(out io.Writer, remote string, r store.PullResult) {
	switch r.Outcome {
	case store.PullRemoteEmpty:
		report(out, "pull", remote+" has no plans yet")
	case store.PullUpToDate:
		report(out, "pull", "up to date with "+remote)
	case store.PullCreated:
		report(out, "pull", "took the plans of "+remote+" ("+short(r.Tip)+")")
	case store.PullFastForward:
		report(out, "pull", "fast-forwarded to "+remote+" ("+short(r.Tip)+")")
	case store.PullMerged:
		report(out, "pull", "merged "+remote+" ("+short(r.Tip)+")")
		for _, p := range r.Kept {
			report(out, "kept", p)
		}
	}
}

func reportPush(out io.Writer, remote string, r store.PushResult) {
	switch r.Outcome {
	case store.PushUpToDate:
		report(out, "push", remote+" is up to date")
	case store.Pushed:
		what := "pushed to " + remote + " (" + short(r.Tip) + ")"
		if r.Merges > 0 {
			what += fmt.Sprintf(" after merging changes pushed meanwhile (%d attempts)", r.Attempts)
		}
		report(out, "push", what)
	}
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
