package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/admSla99/BeforeCode/internal/store"
)

// Locate tells, from a subdirectory, the repository root, the git directory
// of the working tree, the plans ref tip and the current branch.
func TestLocateFindsTheRootTheGitDirTheTipAndTheBranch(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	ctx := context.Background()
	tip, err := open(t, dir).Write(ctx, "first", map[string][]byte{"format-version": []byte("1\n")})
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "src")
	git(t, dir, "switch", "-q", "-c", "feat/x")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	l, err := store.Locate(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if l.Root != git(t, dir, "rev-parse", "--show-toplevel") || l.GitDir != git(t, dir, "rev-parse", "--absolute-git-dir") ||
		l.Tip != tip || l.Head != "refs/heads/feat/x" {
		t.Errorf("Locate = %+v, want tip %s", l, tip)
	}

	git(t, dir, "switch", "-q", "--detach")
	if l, err := store.Locate(ctx, dir); err != nil || l.Head != "" {
		t.Errorf("detached HEAD: Locate = %+v, %v", l, err)
	}
}

func TestLocateWithoutAPlansRefReportsNotInitialised(t *testing.T) {
	t.Parallel()
	if _, err := store.Locate(context.Background(), newRepo(t)); !errors.Is(err, store.ErrNotInitialised) {
		t.Errorf("Locate = %v, want ErrNotInitialised", err)
	}
}
