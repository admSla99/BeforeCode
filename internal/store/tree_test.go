package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/admSla99/BeforeCode/internal/store"
)

// at sets the commit time of the next writes.
func at(t *testing.T, minute int) time.Time {
	t.Helper()
	ts := time.Date(2026, 11, 16, 9, minute, 0, 0, time.UTC)
	t.Setenv("GIT_COMMITTER_DATE", ts.Format(time.RFC3339))
	t.Setenv("GIT_AUTHOR_DATE", ts.Format(time.RFC3339))
	return ts
}

func TestEntriesAndReadAtSeeOneCommitWhileTheRefMoves(t *testing.T) {
	ctx := context.Background()
	s := open(t, newRepo(t))
	first, err := s.Write(ctx, "one", map[string][]byte{"plans/a/intent.md": []byte("v1\n"), "format-version": []byte("1\n")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(ctx, "two", map[string][]byte{"plans/a/intent.md": []byte("v2\n"), "plans/b/intent.md": []byte("b\n")}); err != nil {
		t.Fatal(err)
	}

	tip, err := s.Tip(ctx)
	if err != nil || tip == first {
		t.Fatalf("Tip = %q, %v; want the second commit", tip, err)
	}
	got, err := s.Entries(ctx, first, "plans")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "a" || !got[0].Dir || got[0].ID == "" {
		t.Fatalf("Entries(first, plans) = %+v, want only directory a", got)
	}
	now, err := s.Entries(ctx, tip, "plans")
	if err != nil {
		t.Fatal(err)
	}
	if len(now) != 2 || now[0].ID == got[0].ID {
		t.Errorf("Entries(tip, plans) = %+v, want a (changed tree) and b", now)
	}
	files, err := s.Entries(ctx, tip, "plans/a")
	if err != nil || len(files) != 1 || files[0].Name != "intent.md" || files[0].Dir {
		t.Errorf("Entries(tip, plans/a) = %+v, %v", files, err)
	}
	if data, err := s.ReadAt(ctx, first, "plans/a/intent.md"); err != nil || string(data) != "v1\n" {
		t.Errorf("ReadAt(first) = %q, %v; want v1", data, err)
	}
	if _, err := s.Entries(ctx, tip, "plans/missing"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Entries of a missing directory: %v, want ErrNotFound", err)
	}
}

func TestTipOfAMissingRef(t *testing.T) {
	s := open(t, newRepo(t))
	if _, err := s.Tip(context.Background()); !errors.Is(err, store.ErrNotInitialised) {
		t.Errorf("Tip = %v, want ErrNotInitialised", err)
	}
}

func TestLastChangedSinceACommitSeesOnlyNewerCommits(t *testing.T) {
	ctx := context.Background()
	s := open(t, newRepo(t))
	t1 := at(t, 1)
	write(t, s, "a", map[string]string{"plans/a/intent.md": "a"})
	write(t, s, "b", map[string]string{"plans/b/intent.md": "b"})
	mid, _ := s.Tip(ctx)
	t3 := at(t, 3)
	write(t, s, "e", map[string]string{"plans/b/events/x.yaml": "e"})
	tip, _ := s.Tip(ctx)

	all, err := s.LastChanged(ctx, "", tip, "plans")
	if err != nil {
		t.Fatal(err)
	}
	if !all["a"].Equal(t1) || !all["b"].Equal(t3) || len(all) != 2 {
		t.Errorf("LastChanged from the root = %v", all)
	}
	since, err := s.LastChanged(ctx, mid, tip, "plans")
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 1 || !since["b"].Equal(t3) {
		t.Errorf("LastChanged since mid = %v, want only b", since)
	}
	if ok, err := s.IsAncestor(ctx, mid, tip); !ok || err != nil {
		t.Errorf("IsAncestor(mid, tip) = %v, %v", ok, err)
	}
	if ok, err := s.IsAncestor(ctx, tip, mid); ok || err != nil {
		t.Errorf("IsAncestor(tip, mid) = %v, %v", ok, err)
	}
}

func TestCommonDirIsTheAbsoluteGitDirectory(t *testing.T) {
	dir := newRepo(t)
	got, err := open(t, dir).CommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(dir, ".git"))
	if g, _ := filepath.EvalSymlinks(got); g != want {
		t.Errorf("CommonDir = %q, want %q", got, want)
	}
}

func TestBlobsReadsManyFilesInOneGo(t *testing.T) {
	ctx := context.Background()
	s := open(t, newRepo(t))
	if _, err := s.Write(ctx, "files", map[string][]byte{"a": []byte("first\n"), "b": {}, "c": []byte("line\nwith\x00nul")}); err != nil {
		t.Fatal(err)
	}
	tip, _ := s.Tip(ctx)
	entries, err := s.Entries(ctx, tip, "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	byName := map[string]string{}
	for _, e := range entries {
		ids = append(ids, e.ID)
		byName[e.Name] = e.ID
	}
	got, err := s.Blobs(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a": "first\n", "b": "", "c": "line\nwith\x00nul"} {
		if b, ok := got[byName[name]]; !ok || string(b) != want {
			t.Errorf("blob %s = %q (found %v), want %q", name, b, ok, want)
		}
	}
	if _, err := s.Blobs(ctx, []string{strings.Repeat("0", 40)}); err == nil {
		t.Error("Blobs of a missing object succeeded")
	}
	if got, err := s.Blobs(ctx, nil); err != nil || len(got) != 0 {
		t.Errorf("Blobs(nil) = %v, %v", got, err)
	}
}
