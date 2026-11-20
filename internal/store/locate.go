package store

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Location is where a working tree stands: what a caller that must be fast,
// such as the edit hook, needs to tell whether anything changed since it
// last looked.
type Location struct {
	Root   string // the repository root, as git reports it
	GitDir string // the absolute git directory of this working tree
	Tip    string // the commit of the plans ref
	Head   string // the full name of the current branch, "" for a detached HEAD
}

// Locate reads the Location of the repository around dir with two git
// commands. Unlike Open it does not check the git version; it is meant for
// a repository that a full check (Open) has seen before. A missing plans
// ref is ErrNotInitialised.
func Locate(ctx context.Context, dir string, env ...string) (Location, error) {
	s := &Store{dir: dir, env: env}
	out, stderr, err := s.run(ctx, nil, nil, "rev-parse", "--show-toplevel", "--absolute-git-dir", Ref+"^{commit}")
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err != nil || len(lines) != 3 {
		// The first two lines come before a missing ref is reported.
		if len(lines) >= 2 && strings.Contains(string(stderr), Ref) {
			return Location{}, ErrNotInitialised
		}
		return Location{}, fmt.Errorf("store: cannot locate the repository around %s (git: %s)", dir, firstGitLine(stderr, err))
	}
	l := Location{Root: lines[0], GitDir: lines[1], Tip: lines[2]}
	s.dir = l.Root
	head, err := s.gitLine(ctx, nil, nil, "symbolic-ref", "--quiet", "HEAD")
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1: // detached
	case err != nil:
		return Location{}, err
	default:
		l.Head = head
	}
	return l, nil
}
