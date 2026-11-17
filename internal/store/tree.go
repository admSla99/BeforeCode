package store

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Entry is one entry of a directory in the plans ref.
type Entry struct {
	Name string
	Dir  bool
	// ID is the git object ID: a tree for a directory, a blob for a file.
	// It changes whenever anything beneath the entry changes.
	ID string
}

// Tip returns the commit the plans ref points to, or ErrNotInitialised.
func (s *Store) Tip(ctx context.Context) (string, error) {
	return s.tip(ctx)
}

// Entries returns the entries directly inside dir in commit, in git's tree
// order (byte order). An empty dir lists the root.
func (s *Store) Entries(ctx context.Context, commit, dir string) ([]Entry, error) {
	dir = strings.TrimSuffix(dir, "/")
	tree := commit + "^{tree}"
	if dir != "" {
		e, err := s.entry(ctx, commit, dir)
		if err != nil {
			return nil, err
		}
		if e.typ != "tree" {
			return nil, fmt.Errorf("%w: %s is not a directory", ErrNotFound, dir)
		}
		tree = e.id
	}
	out, err := s.git(ctx, nil, nil, "ls-tree", "-z", tree)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, e := range parseLsTree(out) {
		entries = append(entries, Entry{Name: e.name, Dir: e.typ == "tree", ID: e.id})
	}
	return entries, nil
}

// IsAncestor reports whether commit a is an ancestor of commit b, or b
// itself.
func (s *Store) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	_, stderr, err := s.run(ctx, nil, nil, "merge-base", "--is-ancestor", a, b)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base --is-ancestor: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
}

// CommonDir returns the absolute path of the repository's git directory,
// shared by all its worktrees. Local caches of bfc live beneath it.
func (s *Store) CommonDir(ctx context.Context) (string, error) {
	return s.gitLine(ctx, nil, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
}
