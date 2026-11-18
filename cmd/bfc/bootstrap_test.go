package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/store"
)

// plainClone clones bare as a new team member would, without bfc.
func plainClone(t *testing.T, bare string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ben")
	gitIn(t, ".", "clone", "-q", bare, dir)
	gitIn(t, dir, "config", "user.name", "Ben Bitdiddle")
	gitIn(t, dir, "config", "user.email", "ben@example.com")
	return dir
}

// sharedProject sets up BeforeCode in a clone, commits its config, creates a
// plan and pushes everything. It returns the clone and the bare remote.
func sharedProject(t *testing.T, title string) (ana, bare string) {
	t.Helper()
	dir := copyFixture(t, "shared-"+strings.ReplaceAll(title, " ", "-"), func(dir string) error {
		return buildSharedProject(dir, title)
	})
	return filepath.Join(dir, "work"), filepath.Join(dir, "remote.git")
}

// buildSharedProject makes the repositories of sharedProject in dir.
func buildSharedProject(dir, title string) error {
	if err := buildClone(dir); err != nil {
		return err
	}
	ana := filepath.Join(dir, "work")
	if err := bfcSteps([]string{ana, "init"}); err != nil {
		return err
	}
	if err := gitSteps(
		[]string{ana, "add", ".beforecode"},
		[]string{ana, "commit", "-q", "-m", "Set up BeforeCode"},
		[]string{ana, "push", "-q", "origin", "main"},
	); err != nil {
		return err
	}
	return bfcSteps([]string{ana, "new", "--no-edit", title}, []string{ana, "push"})
}

// AC-1 and AC-6: the full path from a plain clone to the plans.
func TestBootstrapAPlainCloneGetsThePlans(t *testing.T) {
	t.Parallel()
	ana, bare := sharedProject(t, "Payments via Stripe")
	ben := plainClone(t, bare)
	head := gitIn(t, ben, "rev-parse", "HEAD")

	out := mustBfc(t, ben, "bootstrap")
	for _, want := range []string{"origin has plans", "added    fetch refspec " + store.FetchRefspec, "created  " + plansRef} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got, want := gitIn(t, ben, "rev-parse", plansRef), gitIn(t, ana, "rev-parse", plansRef); got != want {
		t.Errorf("ben's plans ref = %s, want ana's %s", got, want)
	}
	if !slices.Contains(fetchRefspecs(t, ben), store.FetchRefspec) {
		t.Errorf("refspec missing: %v", fetchRefspecs(t, ben))
	}
	if got := gitIn(t, ben, "rev-parse", "HEAD"); got != head {
		t.Error("bootstrap moved HEAD")
	}
	if status := gitIn(t, ben, "status", "--porcelain"); status != "" {
		t.Errorf("bootstrap changed the working tree: %q", status)
	}
	if out := mustBfc(t, ben, "list"); !strings.Contains(out, "Payments via Stripe") {
		t.Errorf("list after bootstrap:\n%s", out)
	}
	// A later git fetch keeps the remote plans up to date by itself.
	mustBfc(t, ana, "new", "--no-edit", "Second plan")
	mustBfc(t, ana, "push")
	gitIn(t, ben, "fetch", "-q", "origin")
	if got, want := gitIn(t, ben, "rev-parse", store.RemoteRef), gitIn(t, ana, "rev-parse", plansRef); got != want {
		t.Errorf("git fetch did not update %s", store.RemoteRef)
	}
}

// AC-2
func TestBootstrapWithoutPlansOnTheRemotePointsToInit(t *testing.T) {
	t.Parallel()
	ana, _ := newClone(t)
	out, err := bfcIn(t, ana, "bootstrap")
	if err == nil || !strings.Contains(out, "origin has no plans") || !strings.Contains(out, "bfc init") {
		t.Errorf("bootstrap: %v\n%s", err, out)
	}
	if slices.Contains(fetchRefspecs(t, ana), store.FetchRefspec) {
		t.Errorf("bootstrap added the refspec: %v", fetchRefspecs(t, ana))
	}
	if _, err := gitTry(ana, "rev-parse", "--verify", plansRef); err == nil {
		t.Error("bootstrap created the plans ref")
	}
}

// AC-3
func TestBootstrapTwiceBehavesLikePull(t *testing.T) {
	t.Parallel()
	ana, bare := sharedProject(t, "First")
	ben := plainClone(t, bare)
	mustBfc(t, ben, "bootstrap")
	mustBfc(t, ana, "new", "--no-edit", "Second")
	mustBfc(t, ana, "push")

	out := mustBfc(t, ben, "bootstrap")
	for _, want := range []string{"already set up", "fast-forwarded to origin", "kept     fetch refspec"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if out := mustBfc(t, ben, "list"); !strings.Contains(out, "Second") {
		t.Errorf("list:\n%s", out)
	}
}

// AC-4
func TestBootstrapRefusesANewerFormatVersion(t *testing.T) {
	t.Parallel()
	ana, bare := sharedProject(t, "First")
	writeRef(t, ana, "from the future", map[string][]byte{"format-version": []byte("99\n")})
	mustBfc(t, ana, "push")
	ben := plainClone(t, bare)

	out, err := bfcIn(t, ben, "bootstrap")
	if err == nil || !strings.Contains(out, "format-version 99") || !strings.Contains(out, "update bfc") {
		t.Errorf("bootstrap: %v\n%s", err, out)
	}
	if _, err := gitTry(ben, "rev-parse", "--verify", plansRef); err == nil {
		t.Error("bootstrap created the plans ref")
	}
	if slices.Contains(fetchRefspecs(t, ben), store.FetchRefspec) {
		t.Errorf("bootstrap added the refspec: %v", fetchRefspecs(t, ben))
	}
}

func TestBootstrapUsesTheRemoteFromTheConfig(t *testing.T) {
	t.Parallel()
	ana, bare := newClone(t)
	gitIn(t, ana, "remote", "rename", "origin", "upstream")
	mustBfc(t, ana, "init", "--remote", "upstream")
	gitIn(t, ana, "add", ".beforecode")
	gitIn(t, ana, "commit", "-q", "-m", "Set up BeforeCode")
	gitIn(t, ana, "push", "-q", "upstream", "main")
	mustBfc(t, ana, "push")

	ben := plainClone(t, bare)
	gitIn(t, ben, "remote", "rename", "origin", "upstream")
	if out := mustBfc(t, ben, "bootstrap"); !strings.Contains(out, "upstream has plans") {
		t.Errorf("bootstrap:\n%s", out)
	}
}

// AC-5: commands in a clone that lacks the local ref say which command fits.
func TestCommandsInAFreshCloneSuggestBootstrap(t *testing.T) {
	t.Parallel()
	_, bare := sharedProject(t, "First")
	ben := plainClone(t, bare)
	for _, args := range [][]string{{"list"}, {"show", "bfc-0000"}, {"new", "--no-edit", "x"}, {"edit", "bfc-0000"}, {"push"}} {
		out, err := bfcIn(t, ben, args...)
		if err == nil || !strings.Contains(out, "origin already has plans") || !strings.Contains(out, "bfc bootstrap") {
			t.Errorf("bfc %v: %v\n%s", args, err, out)
		}
	}
}

func TestCommandsWithoutPlansAnywhereSuggestInit(t *testing.T) {
	t.Parallel()
	ana, _ := newClone(t)
	out, err := bfcIn(t, ana, "list")
	if err == nil || !strings.Contains(out, "bfc init") || strings.Contains(out, "bootstrap") {
		t.Errorf("list: %v\n%s", err, out)
	}
}

func TestCommandsWithAnUnreachableRemoteFallBackToTheGeneralAdvice(t *testing.T) {
	t.Parallel()
	ana, _ := newClone(t)
	gitIn(t, ana, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	out, err := bfcIn(t, ana, "list")
	if err == nil || !strings.Contains(out, "bfc init") || !strings.Contains(out, "bfc bootstrap") {
		t.Errorf("list: %v\n%s", err, out)
	}
}
