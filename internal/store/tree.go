package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
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

// Blobs returns the content of the blobs with these object IDs, read with
// one git call. It fails if any of them is missing or not a blob.
func (s *Store) Blobs(ctx context.Context, ids []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	data, err := s.git(ctx, nil, []byte(strings.Join(ids, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	// Each object is "<id> <type> <size>\n<content>\n"; a missing one is
	// "<name> missing\n".
	for len(data) > 0 {
		header, rest, ok := bytes.Cut(data, []byte("\n"))
		if !ok {
			return nil, fmt.Errorf("git cat-file --batch: truncated output")
		}
		f := strings.Fields(string(header))
		if len(f) != 3 || f[1] != "blob" {
			return nil, fmt.Errorf("%w: object %s is not a blob (%s)", ErrNotFound, f[0], header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size+1 > len(rest) {
			return nil, fmt.Errorf("git cat-file --batch: bad header %q", header)
		}
		out[f[0]] = rest[:size:size]
		data = rest[size+1:]
	}
	return out, nil
}

// Existing returns those of paths that are files in commit, in the order
// given, with one git call. The paths go to git on stdin, so there may be any
// number of them.
func (s *Store) Existing(ctx context.Context, commit string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var in strings.Builder
	for _, p := range paths {
		in.WriteString(commit + ":" + p + "\n")
	}
	out, err := s.git(ctx, nil, []byte(in.String()), "cat-file", "--batch-check=%(objecttype)")
	if err != nil {
		return nil, err
	}
	// One line per path: the object type, or "<name> missing".
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(paths) {
		return nil, fmt.Errorf("git cat-file --batch-check: %d lines for %d paths", len(lines), len(paths))
	}
	var found []string
	for i, line := range lines {
		if line == "blob" {
			found = append(found, paths[i])
		}
	}
	return found, nil
}
