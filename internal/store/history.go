package store

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// ObjectID returns the git object ID of the file at path at the current tip
// of the plans ref, or ErrNotFound.
func (s *Store) ObjectID(ctx context.Context, path string) (string, error) {
	tip, err := s.tip(ctx)
	if err != nil {
		return "", err
	}
	e, err := s.entry(ctx, tip, path)
	if err != nil {
		return "", err
	}
	if e.typ != "blob" {
		return "", ErrNotFound
	}
	return e.id, nil
}

// LastChanged returns, for each entry directly inside dir, the commit time
// of the newest commit on the plans ref that changed anything beneath it.
// It reads the history once, so it costs one git call for all entries.
func (s *Store) LastChanged(ctx context.Context, dir string) (map[string]time.Time, error) {
	tip, err := s.tip(ctx)
	if err != nil {
		return nil, err
	}
	dir = strings.TrimSuffix(dir, "/")
	// Each commit prints "\x00<unix time>" followed by the paths it changed.
	out, err := s.git(ctx, nil, nil, "log", "--format=%x00%ct", "--name-only", "--no-renames", tip, "--", dir)
	if err != nil {
		return nil, err
	}
	changed := map[string]time.Time{}
	for _, commit := range strings.Split(string(out), "\x00")[1:] {
		lines := strings.Split(strings.TrimSpace(commit), "\n")
		secs, err := strconv.ParseInt(lines[0], 10, 64)
		if err != nil {
			continue
		}
		when := time.Unix(secs, 0).UTC()
		for _, path := range lines[1:] {
			rest, ok := strings.CutPrefix(path, dir+"/")
			if !ok {
				continue
			}
			child, _, _ := strings.Cut(rest, "/")
			if _, seen := changed[child]; !seen { // the log is newest first
				changed[child] = when
			}
		}
	}
	return changed, nil
}
