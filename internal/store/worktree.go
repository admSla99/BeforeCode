package store

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
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

// Switch switches the working tree to branch with git switch, which
// carries uncommitted changes or refuses; with create it first makes the
// branch at HEAD. A refusal is returned with git's own message.
func (s *Store) Switch(ctx context.Context, branch string, create bool) error {
	args := []string{"switch", "--quiet"}
	if create {
		args = append(args, "--create")
	}
	_, stderr, err := s.run(ctx, nil, nil, append(args, branch)...)
	if err != nil {
		if msg := strings.TrimSpace(string(stderr)); msg != "" {
			return errors.New(msg)
		}
		return fmt.Errorf("git switch %s: %w", branch, err)
	}
	return nil
}
