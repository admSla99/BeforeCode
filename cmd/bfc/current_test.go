package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC-3 and AC-5: bfc use sets the Current plan, which bfc show prints
// without an ID; bfc use --clear removes it again.
func TestUseSetsTheCurrentPlanThatShowPrints(t *testing.T) {
	dir := newLocalRepo(t)
	newPlan(t, dir, "Other")
	short, id := newPlan(t, dir, "Payments")
	head := gitIn(t, dir, "symbolic-ref", "HEAD")

	mustBfc(t, dir, "use", short)

	if got := strings.TrimSpace(readFile(t, filepath.Join(dir, ".git", "beforecode", "current"))); got != id.String() {
		t.Errorf("current file = %q, want %s", got, id)
	}
	if out := mustBfc(t, dir, "show"); !strings.HasPrefix(out, short+"  Payments\n") {
		t.Errorf("show:\n%s", out)
	}
	if got := gitIn(t, dir, "symbolic-ref", "HEAD"); got != head {
		t.Errorf("bfc use changed the branch to %s", got)
	}

	mustBfc(t, dir, "use", "--clear")

	if _, err := os.Stat(filepath.Join(dir, ".git", "beforecode", "current")); !os.IsNotExist(err) {
		t.Errorf("current file after --clear: %v", err)
	}
	out, err := bfcIn(t, dir, "show")
	if err == nil || !strings.Contains(out, "bfc start <id>") || !strings.Contains(out, "bfc use <id>") {
		t.Errorf("show without a Current plan: %v\n%s", err, out)
	}
}

// commit makes a commit in dir so that branches can be made from it.
func commit(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "start")
}

// AC-4: the Short ID in the branch name wins over bfc use; one that names
// no plan is reported; a detached HEAD uses bfc use only.
func TestTheBranchNameWinsAndIsReportedWhenItNamesNoPlan(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	other, _ := newPlan(t, dir, "Other")
	short, _ := newPlan(t, dir, "Payments")
	mustBfc(t, dir, "use", other)

	gitIn(t, dir, "switch", "-q", "-c", "feat/"+short)
	if out := mustBfc(t, dir, "show"); !strings.HasPrefix(out, short+"  Payments\n") {
		t.Errorf("show on feat/%s:\n%s", short, out)
	}

	gitIn(t, dir, "switch", "-q", "-c", "feat/bfc-zzzz-typo")
	out, err := bfcIn(t, dir, "show")
	if err == nil || !strings.Contains(out, "feat/bfc-zzzz-typo names bfc-zzzz, which is no plan") {
		t.Errorf("show on a branch with an unknown Short ID: %v\n%s", err, out)
	}

	gitIn(t, dir, "switch", "-q", "--detach", "feat/"+short)
	if out := mustBfc(t, dir, "show"); !strings.HasPrefix(out, other+"  Other\n") {
		t.Errorf("show on a detached HEAD:\n%s", out)
	}
}

// AC-1 and AC-2: bfc start makes the branch <Short ID>-<slug of the title>
// from HEAD, switches to it, and writes nothing to the plans ref.
func TestStartCreatesTheBranchOfThePlanFromHead(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	head := gitIn(t, dir, "rev-parse", "HEAD")
	short, _ := newPlan(t, dir, "Platby cez Stripe: rýchlo & bezpečne!")
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := mustBfc(t, dir, "start", short)

	want := short + "-platby-cez-stripe-rychlo-bezpecne"
	if got := gitIn(t, dir, "symbolic-ref", "--short", "HEAD"); got != want {
		t.Errorf("branch = %s, want %s", got, want)
	}
	if !strings.Contains(out, want) {
		t.Errorf("start says nothing about the branch:\n%s", out)
	}
	if got := gitIn(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("the branch starts at %s, want HEAD %s", got, head)
	}
	if got := gitIn(t, dir, "rev-parse", plansRef); got != before {
		t.Error("bfc start changed the plans ref")
	}
	if out := mustBfc(t, dir, "show"); !strings.HasPrefix(out, short+"  ") {
		t.Errorf("the plan is not the Current plan:\n%s", out)
	}
}

// AC-1: a local branch that carries the Short ID is reused, under any name.
func TestStartSwitchesToTheBranchThatAlreadyCarriesTheShortID(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	short, _ := newPlan(t, dir, "Payments")
	gitIn(t, dir, "branch", "feat/"+short)

	out := mustBfc(t, dir, "start", short)

	if got := gitIn(t, dir, "symbolic-ref", "--short", "HEAD"); got != "feat/"+short {
		t.Errorf("branch = %s, want feat/%s\n%s", got, short, out)
	}
	if out := mustBfc(t, dir, "start", short); !strings.Contains(out, "already on feat/"+short) {
		t.Errorf("second start:\n%s", out)
	}
	if n := len(strings.Fields(gitIn(t, dir, "branch", "--format=%(refname:short)", "--list", "*"+short+"*"))); n != 1 {
		t.Errorf("start made another branch: %s", gitIn(t, dir, "branch"))
	}
}

// AC-1: when git refuses to switch, the error is shown and nothing changes.
func TestStartShowsGitsRefusalAndChangesNothing(t *testing.T) {
	dir := newLocalRepo(t)
	writeFile(t, filepath.Join(dir, "a.txt"), "main\n")
	gitIn(t, dir, "add", "a.txt")
	commit(t, dir)
	short, _ := newPlan(t, dir, "Payments")
	gitIn(t, dir, "switch", "-q", "-c", "feat/"+short)
	writeFile(t, filepath.Join(dir, "a.txt"), "feature\n")
	gitIn(t, dir, "commit", "-q", "-am", "feature")
	gitIn(t, dir, "switch", "-q", "main")
	writeFile(t, filepath.Join(dir, "a.txt"), "uncommitted\n")

	out, err := bfcIn(t, dir, "start", short)

	if err == nil || !strings.Contains(out, "a.txt") {
		t.Errorf("start over conflicting changes: %v\n%s", err, out)
	}
	if got := gitIn(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("branch = %s, want main", got)
	}
	if got := readFile(t, filepath.Join(dir, "a.txt")); got != "uncommitted\n" {
		t.Errorf("a.txt = %q", got)
	}
}

// AC-2: a closed plan cannot be started; any other state can.
func TestStartRefusesAClosedPlan(t *testing.T) {
	dir, short, _ := draftWithPlan(t)
	commit(t, dir)
	mustBfc(t, dir, "submit", short)
	mustBfc(t, dir, "start", short)
	gitIn(t, dir, "switch", "-q", "main")
	mustBfc(t, dir, "close", short)

	out, err := bfcIn(t, dir, "start", short)

	if err == nil || !strings.Contains(out, "closed") {
		t.Errorf("start of a closed plan: %v\n%s", err, out)
	}
	if got := gitIn(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("branch = %s, want main", got)
	}
}

// AC-1: a branch named with a longer prefix of the plan's ULID, which
// settles a shared Short ID (ADR 0009), is the plan's branch too.
func TestStartReusesABranchNamedWithALongerPrefix(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	short, id := newPlan(t, dir, "Payments")
	longer := "fix/" + short + strings.ToLower(id.String()[14:15])
	gitIn(t, dir, "branch", longer)

	mustBfc(t, dir, "start", short)

	if got := gitIn(t, dir, "symbolic-ref", "--short", "HEAD"); got != longer {
		t.Errorf("branch = %s, want %s", got, longer)
	}
}

// AC-1: uncommitted changes come along, as with git switch.
func TestStartCarriesUncommittedChanges(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	short, _ := newPlan(t, dir, "Payments")
	writeFile(t, filepath.Join(dir, "wip.txt"), "draft\n")

	mustBfc(t, dir, "start", short)

	if got := readFile(t, filepath.Join(dir, "wip.txt")); got != "draft\n" {
		t.Errorf("wip.txt = %q", got)
	}
}

// AC-3: bfc use writes nothing to the plans ref, and says when the branch
// name wins over it.
func TestUseOnABranchOfAnotherPlanSaysTheBranchWins(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	other, _ := newPlan(t, dir, "Other")
	short, _ := newPlan(t, dir, "Payments")
	mustBfc(t, dir, "start", short)
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := mustBfc(t, dir, "use", other)

	if !strings.Contains(out, "the branch "+gitIn(t, dir, "symbolic-ref", "--short", "HEAD")+" names "+short+", which stays the Current plan") {
		t.Errorf("use on a plan branch:\n%s", out)
	}
	if got := gitIn(t, dir, "rev-parse", plansRef); got != before {
		t.Error("bfc use changed the plans ref")
	}
}

// AC-4: only a whole Short ID of at least four characters counts, so a
// branch such as release/bfc-v2 has no Short ID.
func TestABranchWithATooShortBfcWordHasNoShortID(t *testing.T) {
	dir := newLocalRepo(t)
	commit(t, dir)
	short, _ := newPlan(t, dir, "Payments")
	mustBfc(t, dir, "use", short)
	gitIn(t, dir, "switch", "-q", "-c", "release/bfc-v2")

	if out := mustBfc(t, dir, "show"); !strings.HasPrefix(out, short+"  Payments\n") {
		t.Errorf("show on release/bfc-v2:\n%s", out)
	}
}
