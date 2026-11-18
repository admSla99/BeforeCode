package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// This file shares the plans ref through a git remote: Pull, Push and Sync.
// It prints nothing; the CLI and the TUI report the results. The merge and
// retry policy is ADR 0004.

// RemoteRef is where Pull keeps the remote's plans ref, as FetchRefspec does.
const RemoteRef = "refs/beforecode-remote/plans"

// MaxPushAttempts bounds how often Push tries before it gives up because
// other writers keep winning the race.
const MaxPushAttempts = 5

// ErrPushRetriesExhausted is returned when every one of MaxPushAttempts
// pushes was rejected because the remote had moved.
var ErrPushRetriesExhausted = fmt.Errorf("store: the remote plans ref kept changing; gave up after %d attempts", MaxPushAttempts)

// Keep says which side wins in files that both sides changed differently.
type Keep int

const (
	// KeepNeither stops at a conflict and commits nothing.
	KeepNeither Keep = iota
	// KeepLocal takes the local version of every conflicting file.
	KeepLocal
	// KeepRemote takes the remote's version of every conflicting file.
	KeepRemote
)

func (k Keep) String() string {
	switch k {
	case KeepLocal:
		return "local"
	case KeepRemote:
		return "remote"
	default:
		return "neither"
	}
}

// PullOutcome says what Pull did to the local plans ref.
type PullOutcome int

const (
	// PullRemoteEmpty: the remote has no plans ref; nothing changed.
	PullRemoteEmpty PullOutcome = iota
	// PullUpToDate: the local ref already contains the remote's.
	PullUpToDate
	// PullCreated: there was no local ref; it now equals the remote's.
	PullCreated
	// PullFastForward: the local ref moved forward to the remote's.
	PullFastForward
	// PullMerged: a merge commit joins the local and the remote history.
	PullMerged
)

// PullResult is what Pull did.
type PullResult struct {
	Outcome PullOutcome
	// Tip is the local plans ref afterwards, empty for PullRemoteEmpty
	// without a local ref.
	Tip string
	// Kept lists the conflicting files resolved with Keep.
	Kept []string
}

// PushOutcome says what Push did to the remote.
type PushOutcome int

const (
	// PushUpToDate: the remote already had the local plans ref.
	PushUpToDate PushOutcome = iota
	// Pushed: the remote now has the local plans ref.
	Pushed
)

// PushResult is what Push did.
type PushResult struct {
	Outcome PushOutcome
	// Tip is the commit the remote has afterwards.
	Tip string
	// Attempts counts the pushes tried, at most MaxPushAttempts.
	Attempts int
	// Merges counts the merges with the remote made between attempts.
	Merges int
}

// Conflict is one file that both sides changed in ways that do not merge.
type Conflict struct {
	Path string
	// Local and Remote are the blob IDs of the two versions, empty for a
	// side that deleted the file.
	Local, Remote string
}

// ConflictError is returned when the local and the remote plans ref changed
// the same file differently. Nothing was committed.
type ConflictError struct {
	Remote                    string
	LocalCommit, RemoteCommit string
	Conflicts                 []Conflict
}

func (e *ConflictError) Error() string {
	paths := make([]string, len(e.Conflicts))
	for i, c := range e.Conflicts {
		paths[i] = c.Path
	}
	return fmt.Sprintf("store: plans ref conflicts with %s in %s; nothing was committed", e.Remote, strings.Join(paths, ", "))
}

// RemoteError is a failed fetch or push, carrying git's own message, for
// example about the network or authentication.
type RemoteError struct {
	Remote string
	// Op is "fetch from" or "push to".
	Op string
	// Message is what git printed.
	Message string
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("cannot %s %s: %s", e.Op, e.Remote, e.Message)
}

// Pull fetches the remote's plans ref into RemoteRef and integrates it into
// the local plans ref: it creates it, fast-forwards it, or merges the two in
// memory with `git merge-tree --write-tree` and a merge commit. On a conflict
// it commits nothing and returns a *ConflictError, unless keep picks a side
// for the conflicting files. The working tree, the index and the current
// branch are not touched.
func (s *Store) Pull(ctx context.Context, remote string, keep Keep) (PullResult, error) {
	if _, err := s.Fetch(ctx, remote); err != nil {
		return PullResult{}, err
	}
	return s.Integrate(ctx, remote, keep)
}

// Fetch brings the remote's plans ref to RemoteRef, and removes RemoteRef if
// the remote has none. It returns the fetched commit, or "" when the remote
// has no plans ref. The local plans ref is not touched.
func (s *Store) Fetch(ctx context.Context, remote string) (string, error) {
	if err := checkRemote(remote); err != nil {
		return "", err
	}
	if err := s.fetch(ctx, remote); err != nil {
		return "", err
	}
	return s.optionalCommit(ctx, RemoteRef)
}

// ReadRemote returns the content of the file at path in RemoteRef, as the
// last Fetch left it.
func (s *Store) ReadRemote(ctx context.Context, path string) ([]byte, error) {
	theirs, err := s.optionalCommit(ctx, RemoteRef)
	if err != nil {
		return nil, err
	}
	if theirs == "" {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, RemoteRef)
	}
	return s.ReadAt(ctx, theirs, path)
}

// Integrate brings RemoteRef, as the last Fetch left it, into the local
// plans ref, as Pull describes. remote names it in messages.
func (s *Store) Integrate(ctx context.Context, remote string, keep Keep) (PullResult, error) {
	theirs, err := s.optionalCommit(ctx, RemoteRef)
	if err != nil {
		return PullResult{}, err
	}
	ours, err := s.optionalCommit(ctx, Ref)
	if err != nil {
		return PullResult{}, err
	}
	switch {
	case theirs == "":
		return PullResult{Outcome: PullRemoteEmpty, Tip: ours}, nil
	case ours == "":
		return PullResult{Outcome: PullCreated, Tip: theirs}, s.moveRef(ctx, "pull "+remote, theirs, "")
	}

	base, err := s.mergeBase(ctx, ours, theirs)
	switch {
	case err != nil:
		return PullResult{}, err
	case base == theirs:
		return PullResult{Outcome: PullUpToDate, Tip: ours}, nil
	case base == ours:
		return PullResult{Outcome: PullFastForward, Tip: theirs}, s.moveRef(ctx, "pull "+remote, theirs, ours)
	}

	tree, conflicts, err := s.mergeTrees(ctx, ours, theirs)
	if err != nil {
		return PullResult{}, err
	}
	message := "bfc: merge " + remote
	var kept []string
	if len(conflicts) > 0 {
		if keep == KeepNeither {
			return PullResult{}, &ConflictError{Remote: remote, LocalCommit: ours, RemoteCommit: theirs, Conflicts: conflicts}
		}
		if tree, err = s.keepSide(ctx, tree, conflicts, keep); err != nil {
			return PullResult{}, err
		}
		for _, c := range conflicts {
			kept = append(kept, c.Path)
		}
		message += fmt.Sprintf("\n\nKept the %s version of: %s", keep, strings.Join(kept, ", "))
	}
	commit, err := s.gitLine(ctx, nil, nil, "commit-tree", tree, "-p", ours, "-p", theirs, "-m", message)
	if err != nil {
		return PullResult{}, err
	}
	if err := s.moveRef(ctx, "merge "+remote, commit, ours); err != nil {
		return PullResult{}, err
	}
	return PullResult{Outcome: PullMerged, Tip: commit, Kept: kept}, nil
}

// Push pushes the local plans ref to the remote, never by force. When the
// remote rejects it because it has commits the local ref lacks, Push pulls
// (without picking a side on a conflict) and tries again, at most
// MaxPushAttempts times.
func (s *Store) Push(ctx context.Context, remote string) (PushResult, error) {
	var r PushResult
	if err := checkRemote(remote); err != nil {
		return r, err
	}
	for r.Attempts < MaxPushAttempts {
		tip, err := s.tip(ctx)
		if err != nil {
			return r, err
		}
		r.Attempts++
		r.Tip = tip
		rejected, upToDate, err := s.push(ctx, remote)
		if err != nil {
			return r, err
		}
		if !rejected {
			r.Outcome = Pushed
			if upToDate {
				r.Outcome = PushUpToDate
			}
			return r, nil
		}
		pulled, err := s.Pull(ctx, remote, KeepNeither)
		if err != nil {
			return r, err
		}
		if pulled.Outcome == PullMerged {
			r.Merges++
		}
	}
	return r, ErrPushRetriesExhausted
}

// Sync pulls and then pushes.
func (s *Store) Sync(ctx context.Context, remote string, keep Keep) (PullResult, PushResult, error) {
	pulled, err := s.Pull(ctx, remote, keep)
	if err != nil {
		return pulled, PushResult{}, err
	}
	pushed, err := s.Push(ctx, remote)
	return pulled, pushed, err
}

// fetch brings the remote's plans ref to RemoteRef, and removes RemoteRef if
// the remote has none. The wildcard refspec does not fail on a remote
// without plans. flags go to git fetch.
func (s *Store) fetch(ctx context.Context, remote string, flags ...string) error {
	args := append([]string{"fetch", "--quiet", "--no-tags", "--prune", "--no-write-fetch-head"}, flags...)
	_, stderr, err := s.run(ctx, nil, nil, append(args, remote, FetchRefspec)...)
	if err != nil {
		return remoteError(remote, "fetch from", stderr, err)
	}
	return nil
}

// push runs one `git push` of the plans ref without force. It reports
// whether the remote rejected it for lacking commits ("fetch first" on
// GitHub, "non-fast-forward" elsewhere) and whether the remote already had
// it.
func (s *Store) push(ctx context.Context, remote string) (rejected, upToDate bool, err error) {
	stdout, stderr, err := s.run(ctx, nil, nil, "push", "--porcelain", remote, Ref+":"+Ref)
	// Porcelain lines: "<flag> TAB <from>:<to> TAB <summary>".
	for _, line := range strings.Split(string(stdout), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 || fields[1] != Ref+":"+Ref {
			continue
		}
		switch {
		case fields[0] == "=":
			upToDate = true
		case fields[0] == "!" && strings.HasPrefix(fields[2], "[rejected]"):
			return true, false, nil
		}
	}
	if err != nil {
		return false, false, remoteError(remote, "push to", stderr, err)
	}
	return false, upToDate, nil
}

// checkRemote rejects a remote name that git would read as an option.
func checkRemote(remote string) error {
	if remote == "" || strings.HasPrefix(remote, "-") {
		return fmt.Errorf("store: invalid remote name %q", remote)
	}
	return nil
}

func remoteError(remote, op string, stderr []byte, err error) error {
	msg := strings.TrimSpace(string(stderr))
	if msg == "" {
		msg = err.Error()
	}
	return &RemoteError{Remote: remote, Op: op, Message: msg}
}

// optionalCommit resolves ref to a commit, or "" if it does not exist.
func (s *Store) optionalCommit(ctx context.Context, ref string) (string, error) {
	commit, err := s.gitLine(ctx, nil, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return commit, err
}

// mergeBase returns the best common ancestor of a and b, or "" when their
// histories are unrelated.
func (s *Store) mergeBase(ctx context.Context, a, b string) (string, error) {
	base, err := s.gitLine(ctx, nil, nil, "merge-base", a, b)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return "", nil
	}
	return base, err
}

// mergeTrees merges two commits in memory. It returns the merged tree, which
// holds conflict markers in conflicting files, and the conflicts.
func (s *Store) mergeTrees(ctx context.Context, ours, theirs string) (string, []Conflict, error) {
	// Exit code 0 is a clean merge, 1 a merge with conflicts; output is
	// "<tree> NUL" followed by "<path> NUL" for each conflicting file.
	stdout, stderr, err := s.run(ctx, nil, nil, "merge-tree", "--write-tree", "-z", "--name-only", "--no-messages",
		"--allow-unrelated-histories", ours, theirs)
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return "", nil, fmt.Errorf("git merge-tree: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	conflicted := err != nil
	fields := strings.Split(strings.TrimRight(string(stdout), "\x00"), "\x00")
	tree := fields[0]
	var conflicts []Conflict
	for _, path := range fields[1:] {
		if path == "" || slices.ContainsFunc(conflicts, func(c Conflict) bool { return c.Path == path }) {
			continue
		}
		c := Conflict{Path: path}
		if c.Local, err = s.blobAt(ctx, ours, path); err != nil {
			return "", nil, err
		}
		if c.Remote, err = s.blobAt(ctx, theirs, path); err != nil {
			return "", nil, err
		}
		conflicts = append(conflicts, c)
	}
	if conflicted != (len(conflicts) > 0) {
		return "", nil, fmt.Errorf("git merge-tree: exit code and the %d conflicting files listed disagree", len(conflicts))
	}
	return tree, conflicts, nil
}

// blobAt returns the blob ID of path in commit, or "" if there is no file
// there.
func (s *Store) blobAt(ctx context.Context, commit, path string) (string, error) {
	e, err := s.entry(ctx, commit, path)
	if errors.Is(err, ErrNotFound) || err == nil && e.typ != "blob" {
		return "", nil
	}
	return e.id, err
}

// keepSide returns tree with each conflicting file replaced by the version
// of the side keep names, or removed where that side has none.
func (s *Store) keepSide(ctx context.Context, tree string, conflicts []Conflict, keep Keep) (string, error) {
	tmp, err := os.MkdirTemp("", "bfc-index-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
	if _, err := s.git(ctx, env, nil, "read-tree", tree); err != nil {
		return "", err
	}
	var entries bytes.Buffer
	for _, c := range conflicts {
		blob := c.Local
		if keep == KeepRemote {
			blob = c.Remote
		}
		// Mode 0 removes the path from the index.
		if blob == "" {
			fmt.Fprintf(&entries, "0 %s\t%s\x00", strings.Repeat("0", len(tree)), c.Path)
		} else {
			fmt.Fprintf(&entries, "100644 %s\t%s\x00", blob, c.Path)
		}
	}
	if _, err := s.git(ctx, env, entries.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return "", err
	}
	return s.gitLine(ctx, env, nil, "write-tree")
}

// moveRef sets the plans ref to commit if it still points to old ("" for a
// ref that must not exist yet).
func (s *Store) moveRef(ctx context.Context, reason, commit, old string) error {
	_, err := s.git(ctx, nil, nil, "update-ref", "-m", "bfc: "+reason, Ref, commit, old)
	return err
}
