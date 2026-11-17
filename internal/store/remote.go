package store

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
)

// FetchRefspec makes `git fetch <remote>` bring the remote's plans ref
// (ADR 0001).
const FetchRefspec = "+refs/beforecode/*:refs/beforecode-remote/*"

// ErrRemoteUnreachable is returned when a remote cannot be asked about its
// refs, for example without network or access.
var ErrRemoteUnreachable = errors.New("store: remote cannot be reached")

// Root returns the top-level directory of the repository's working tree.
func (s *Store) Root() string {
	return s.dir
}

// Initialised reports whether the plans ref exists in this repository.
func (s *Store) Initialised(ctx context.Context) (bool, error) {
	_, err := s.tip(ctx)
	if errors.Is(err, ErrNotInitialised) {
		return false, nil
	}
	return err == nil, err
}

// HasRemote reports whether a git remote with this name is configured.
func (s *Store) HasRemote(ctx context.Context, remote string) (bool, error) {
	out, err := s.git(ctx, nil, nil, "remote")
	if err != nil {
		return false, err
	}
	return slices.Contains(strings.Fields(string(out)), remote), nil
}

// RemoteHasPlans asks the remote whether it already has a plans ref. It
// returns ErrRemoteUnreachable when the remote does not answer.
func (s *Store) RemoteHasPlans(ctx context.Context, remote string) (bool, error) {
	tip, err := s.RemoteTip(ctx, remote)
	return tip != "", err
}

// RemoteTip asks the remote which commit its plans ref points to, without
// fetching it. It returns "" when the remote has no plans ref, and
// ErrRemoteUnreachable when the remote does not answer.
func (s *Store) RemoteTip(ctx context.Context, remote string) (string, error) {
	if err := checkRemote(remote); err != nil {
		return "", err
	}
	out, err := s.git(ctx, nil, nil, "ls-remote", "--exit-code", remote, Ref)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		// "<commit> TAB <ref>"; the ref is matched exactly.
		for _, line := range strings.Split(string(out), "\n") {
			if commit, ref, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && ref == Ref {
				return commit, nil
			}
		}
		return "", nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 2:
		return "", nil
	default:
		return "", fmt.Errorf("%w: %s: %w", ErrRemoteUnreachable, remote, err)
	}
}

// HasCommit reports whether commit is in this repository's object store.
func (s *Store) HasCommit(ctx context.Context, commit string) bool {
	_, _, err := s.run(ctx, nil, nil, "cat-file", "-e", commit+"^{commit}")
	return err == nil
}

// HasFetchRefspec reports whether FetchRefspec is among the remote's fetch
// refspecs in .git/config.
func (s *Store) HasFetchRefspec(ctx context.Context, remote string) (bool, error) {
	out, err := s.git(ctx, nil, nil, "config", "--get-all", "remote."+remote+".fetch")
	// Exit code 1 means the key is not set yet.
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return false, err
	}
	return slices.Contains(strings.Split(strings.TrimSpace(string(out)), "\n"), FetchRefspec), nil
}

// AddFetchRefspec adds FetchRefspec to the remote's fetch refspecs in
// .git/config unless it is already there. It reports whether it added it.
func (s *Store) AddFetchRefspec(ctx context.Context, remote string) (bool, error) {
	has, err := s.HasFetchRefspec(ctx, remote)
	if err != nil || has {
		return false, err
	}
	if _, err := s.git(ctx, nil, nil, "config", "--add", "remote."+remote+".fetch", FetchRefspec); err != nil {
		return false, err
	}
	return true, nil
}
