package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoClones returns a clone set up with bfc init and pushed, a second clone
// that pulled it, and the bare remote.
func twoClones(t *testing.T) (ana, ben, bare string) {
	t.Helper()
	ana, bare = newClone(t)
	if out, err := bfcIn(t, ana, "init"); err != nil {
		t.Fatalf("bfc init: %v\n%s", err, out)
	}
	if out, err := bfcIn(t, ana, "push"); err != nil {
		t.Fatalf("bfc push: %v\n%s", err, out)
	}
	ben = filepath.Join(t.TempDir(), "ben")
	gitIn(t, ".", "clone", "-q", bare, ben)
	gitIn(t, ben, "config", "user.name", "Ben Bitdiddle")
	gitIn(t, ben, "config", "user.email", "ben@example.com")
	if out, err := bfcIn(t, ben, "pull"); err != nil || !strings.Contains(out, "took the plans of origin") {
		t.Fatalf("bfc pull: %v\n%s", err, out)
	}
	return ana, ben, bare
}

func TestPushAndPullShareAPlan(t *testing.T) {
	ana, ben, bare := twoClones(t)
	if got, want := gitIn(t, bare, "rev-parse", plansRef), gitIn(t, ben, "rev-parse", plansRef); got != want {
		t.Fatalf("ben has %s, remote %s", want, got)
	}

	out, err := bfcIn(t, ana, "new", "--no-edit", "Payments via Stripe")
	if err != nil {
		t.Fatalf("bfc new: %v\n%s", err, out)
	}
	out, err = bfcIn(t, ana, "push")
	if err != nil || !strings.Contains(out, "pushed to origin") {
		t.Fatalf("bfc push: %v\n%s", err, out)
	}
	out, err = bfcIn(t, ben, "pull")
	if err != nil || !strings.Contains(out, "fast-forwarded to origin") {
		t.Fatalf("bfc pull: %v\n%s", err, out)
	}
	out, err = bfcIn(t, ben, "list")
	if err != nil || !strings.Contains(out, "Payments via Stripe") {
		t.Errorf("ben's list lacks ana's plan: %v\n%s", err, out)
	}
	if out, _ := bfcIn(t, ben, "pull"); !strings.Contains(out, "up to date with origin") {
		t.Errorf("second pull: %s", out)
	}
	if out, _ := bfcIn(t, ana, "push"); !strings.Contains(out, "origin is up to date") {
		t.Errorf("second push: %s", out)
	}
}

func TestPushAfterSomeoneElsePushedMergesAndRetries(t *testing.T) {
	ana, ben, _ := twoClones(t)
	for dir, title := range map[string]string{ana: "Ana's plan", ben: "Ben's plan"} {
		if out, err := bfcIn(t, dir, "new", "--no-edit", title); err != nil {
			t.Fatalf("bfc new: %v\n%s", err, out)
		}
	}
	if out, err := bfcIn(t, ana, "push"); err != nil {
		t.Fatalf("ana push: %v\n%s", err, out)
	}
	out, err := bfcIn(t, ben, "push")
	if err != nil || !strings.Contains(out, "after merging changes pushed meanwhile (2 attempts)") {
		t.Fatalf("ben push: %v\n%s", err, out)
	}
	if out, err := bfcIn(t, ana, "sync"); err != nil || !strings.Contains(out, "fast-forwarded") {
		t.Fatalf("ana sync: %v\n%s", err, out)
	}
	out, _ = bfcIn(t, ana, "list")
	if !strings.Contains(out, "Ana's plan") || !strings.Contains(out, "Ben's plan") {
		t.Errorf("ana's list lacks a plan:\n%s", out)
	}
}

// editIntent appends text to intent.md of the newest plan in dir's plans
// ref.
func editIntent(t *testing.T, dir, text string) {
	t.Helper()
	fakeEditor(t, `printf '`+text+`' >> "$1"`)
	short := strings.Fields(mustBfc(t, dir, "list"))[0]
	mustBfc(t, dir, "edit", "--intent", short)
}

func mustBfc(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := bfcIn(t, dir, args...)
	if err != nil {
		t.Fatalf("bfc %v: %v\n%s", args, err, out)
	}
	return out
}

func TestPullConflictListsBothVersionsAndKeepResolvesIt(t *testing.T) {
	ana, ben, _ := twoClones(t)
	mustBfc(t, ana, "new", "--no-edit", "Shared plan")
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")
	editIntent(t, ana, `Ana wants this.\n`)
	editIntent(t, ben, `Ben wants that.\n`)
	mustBfc(t, ana, "push")
	before := gitIn(t, ben, "rev-parse", plansRef)

	out, err := bfcIn(t, ben, "pull")
	if err == nil {
		t.Fatalf("pull over a conflict succeeded:\n%s", out)
	}
	for _, want := range []string{"Nothing was committed", "conflict plans/", "/intent.md", "local:  git show", "remote: git show", "bfc pull --keep local", "bfc pull --keep remote"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := gitIn(t, ben, "rev-parse", plansRef); got != before {
		t.Error("a conflicting pull moved the plans ref")
	}
	// The printed commands show each side's version.
	for _, line := range strings.Split(out, "\n") {
		cmd, ok := strings.CutPrefix(strings.TrimSpace(line), "remote: git show ")
		if ok && !strings.Contains(gitIn(t, ben, "show", cmd), "Ana wants this.") {
			t.Errorf("git show %s does not show ana's version", cmd)
		}
	}

	if out, err := bfcIn(t, ben, "push"); err == nil || !strings.Contains(out, "bfc pull --keep") {
		t.Errorf("push over a conflict: %v\n%s", err, out)
	}

	out = mustBfc(t, ben, "sync", "--keep", "remote")
	if !strings.Contains(out, "merged origin") || !strings.Contains(out, "kept") || !strings.Contains(out, "pushed to origin") {
		t.Errorf("sync --keep remote:\n%s", out)
	}
	if out := mustBfc(t, ben, "show", strings.Fields(mustBfc(t, ben, "list"))[0]); !strings.Contains(out, "Ana wants this.") {
		t.Errorf("after --keep remote the intent is:\n%s", out)
	}
}

func TestSyncUsesTheRemoteFromTheConfig(t *testing.T) {
	ana, bare := newClone(t)
	gitIn(t, ana, "remote", "rename", "origin", "upstream")
	mustBfc(t, ana, "init", "--remote", "upstream")
	out := mustBfc(t, ana, "sync")
	if !strings.Contains(out, "upstream has no plans yet") || !strings.Contains(out, "pushed to upstream") {
		t.Errorf("sync:\n%s", out)
	}
	if gitIn(t, bare, "rev-parse", plansRef) != gitIn(t, ana, "rev-parse", plansRef) {
		t.Error("the remote does not have the plans ref")
	}
}

func TestSyncInLocalModeRefusesUnlessARemoteIsNamed(t *testing.T) {
	dir := newLocalRepo(t)
	out, err := bfcIn(t, dir, "push")
	if err == nil || !strings.Contains(out, "local mode") {
		t.Errorf("push in local mode: %v\n%s", err, out)
	}
	if _, err := bfcIn(t, dir, "pull", "--keep", "theirs"); err == nil {
		t.Error("--keep theirs was accepted")
	}
}

func TestPushReportsGitsOwnMessageWhenTheRemoteCannotBeReached(t *testing.T) {
	ana, _ := newClone(t)
	mustBfc(t, ana, "init")
	gitIn(t, ana, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	for _, cmd := range []string{"push", "pull", "sync"} {
		out, err := bfcIn(t, ana, cmd)
		if err == nil || !strings.Contains(out, "does not appear to be a git repository") || !strings.Contains(out, "cannot ") {
			t.Errorf("bfc %s: %v\n%s", cmd, err, out)
		}
	}
}

func TestPushBeforeInitPointsToInit(t *testing.T) {
	ana, _ := newClone(t)
	out, err := bfcIn(t, ana, "push")
	if err == nil || !strings.Contains(out, "bfc init") {
		t.Errorf("push before init: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(ana, ".beforecode")); err == nil {
		t.Error("push created .beforecode")
	}
}
