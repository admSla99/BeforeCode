package store

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// These methods read and change the current branch of the working tree,
// for the Current plan (ADR 0009). They never touch the plans ref.

// GitDir returns the absolute path of the git directory of this working
// tree. Unlike CommonDir it is a worktree's own, so state kept there, such
// as the plan set with bfc use, belongs to one worktree.
func (s *Store) GitDir(ctx context.Context) (string, error) {
	return s.gitLine(ctx, nil, nil, "rev-parse", "--absolute-git-dir")
}

// Branch returns the short name of the current branch, also when it has no
// commits yet, or "" for a detached HEAD.
func (s *Store) Branch(ctx context.Context) (string, error) {
	out, err := s.gitLine(ctx, nil, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return out, err
}

// LocalBranches returns the short names of the local branches.
func (s *Store) LocalBranches(ctx context.Context) ([]string, error) {
	out, err := s.git(ctx, nil, nil, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// Switch switches the working tree to an existing local branch with git
// switch, which carries uncommitted changes or refuses. A refusal is
// returned with git's own message.
func (s *Store) Switch(ctx context.Context, branch string) error {
	return s.switchTo(ctx, branch)
}

// SwitchNew makes the branch at HEAD and switches to it.
func (s *Store) SwitchNew(ctx context.Context, branch string) error {
	return s.switchTo(ctx, "--create", branch)
}

// SwitchTracking makes the local branch of the same name from the
// remote-tracking branch <remote>/<branch>, set to track it, and switches to
// it.
func (s *Store) SwitchTracking(ctx context.Context, remote, branch string) error {
	return s.switchTo(ctx, "--create", branch, "--track", remote+"/"+branch)
}

func (s *Store) switchTo(ctx context.Context, args ...string) error {
	_, stderr, err := s.run(ctx, nil, nil, append([]string{"switch", "--quiet"}, args...)...)
	if err != nil {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("git switch %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// FetchBranches brings the remote's branches to its remote-tracking
// branches (refs/remotes/<remote>/*). It touches no local branch.
func (s *Store) FetchBranches(ctx context.Context, remote string) error {
	_, stderr, err := s.run(ctx, nil, nil, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head",
		remote, "+refs/heads/*:refs/remotes/"+remote+"/*")
	if err != nil {
		return remoteError(remote, "fetch the branches of", stderr, err)
	}
	return nil
}

// RemoteBranches returns the branch names the remote-tracking branches of
// remote stand for, without the "<remote>/" prefix and without HEAD.
func (s *Store) RemoteBranches(ctx context.Context, remote string) ([]string, error) {
	prefix := "refs/remotes/" + remote + "/"
	out, err := s.git(ctx, nil, nil, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ref := range strings.Fields(string(out)) {
		if name := strings.TrimPrefix(ref, prefix); name != "HEAD" {
			names = append(names, name)
		}
	}
	return names, nil
}

// Behind returns how many commits the branch lacks from its upstream, and
// the upstream's short name; "" and 0 when it has no upstream.
func (s *Store) Behind(ctx context.Context, branch string) (upstream string, n int, err error) {
	upstream, err = s.gitLine(ctx, nil, nil, "for-each-ref", "--format=%(upstream:short)", "refs/heads/"+branch)
	if err != nil || upstream == "" {
		return "", 0, err
	}
	if _, err := s.gitLine(ctx, nil, nil, "rev-parse", "--verify", "--quiet", upstream+"^{commit}"); err != nil {
		return "", 0, nil // the upstream is gone
	}
	count, err := s.gitLine(ctx, nil, nil, "rev-list", "--count", branch+".."+upstream)
	if err != nil {
		return "", 0, err
	}
	n, err = strconv.Atoi(count)
	return upstream, n, err
}
