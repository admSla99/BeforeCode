package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/admSla99/BeforeCode/internal/store"
)

func TestRemoteTipAsksTheRemoteWithoutFetching(t *testing.T) {
	t.Parallel()
	_, anaDir, benDir := team(t)
	a, b := open(t, anaDir), open(t, benDir)
	ctx := context.Background()

	if tip, err := b.RemoteTip(ctx, "origin"); err != nil || tip != "" {
		t.Errorf("RemoteTip of a remote without plans = %q, %v; want \"\", nil", tip, err)
	}
	commit := write(t, a, "base", map[string]string{"format-version": "1\n"})
	push(t, a)
	if tip, err := b.RemoteTip(ctx, "origin"); err != nil || tip != commit {
		t.Errorf("RemoteTip = %q, %v; want %s", tip, err, commit)
	}
	if b.HasCommit(ctx, commit) {
		t.Error("RemoteTip fetched the commit")
	}
	if _, err := b.RemoteTip(ctx, "nowhere"); !errors.Is(err, store.ErrRemoteUnreachable) {
		t.Errorf("RemoteTip of a missing remote: %v, want ErrRemoteUnreachable", err)
	}
	pull(t, b, store.KeepNeither)
	if !b.HasCommit(ctx, commit) {
		t.Error("HasCommit is false after a pull")
	}
}

func TestHasFetchRefspecFollowsAddFetchRefspec(t *testing.T) {
	t.Parallel()
	s := open(t, newRepo(t))
	ctx := context.Background()
	git(t, s.Root(), "remote", "add", "origin", "https://example.invalid/repo.git")

	if has, err := s.HasFetchRefspec(ctx, "origin"); err != nil || has {
		t.Fatalf("HasFetchRefspec before = %v, %v", has, err)
	}
	if added, err := s.AddFetchRefspec(ctx, "origin"); err != nil || !added {
		t.Fatalf("AddFetchRefspec = %v, %v", added, err)
	}
	if has, err := s.HasFetchRefspec(ctx, "origin"); err != nil || !has {
		t.Errorf("HasFetchRefspec after = %v, %v", has, err)
	}
}

func TestGitVersionMeetsTheMinimum(t *testing.T) {
	t.Parallel()
	version, ok, err := store.GitVersion(context.Background())
	if err != nil || !ok || version == "" {
		t.Errorf("GitVersion = %q, %v, %v; the test machine has git %s or newer", version, ok, err, store.MinGitVersion)
	}
}
