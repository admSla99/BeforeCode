package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
	"github.com/admSla99/BeforeCode/internal/store"
)

// doctorLine returns the line of bfc doctor's output about check, or "".
func doctorLine(out, check string) string {
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 6 && strings.HasPrefix(line[6:], check+":") {
			return line
		}
	}
	return ""
}

// wantDoctor fails unless the line about check has the status and contains
// every one of want.
func wantDoctor(t *testing.T, out, status, check string, want ...string) {
	t.Helper()
	line := doctorLine(out, check)
	if !strings.HasPrefix(line, status+" ") {
		t.Errorf("%s: want status %s, got line %q in:\n%s", check, status, line, out)
		return
	}
	for _, w := range want {
		if !strings.Contains(line, w) {
			t.Errorf("%s: line %q lacks %q", check, line, w)
		}
	}
}

// AC-1: a healthy clone passes every check, one line each.
func TestDoctorPassesEveryCheckOfAHealthySetup(t *testing.T) {
	ana, _ := sharedProject(t, "Payments via Stripe")

	out := mustBfc(t, ana, "doctor")
	for _, check := range []string{"git version", "repository", "config", "plans ref", "fetch refspec", "remote",
		"in sync", "format-version", "index", "event files", "plans", "skipped events", "short IDs"} {
		wantDoctor(t, out, "ok", check)
	}
	if strings.Contains(out, "fix:") {
		t.Errorf("a healthy setup has nothing to fix:\n%s", out)
	}
}

// AC-2, AC-3, AC-4: a failure prints its fix and fails the run; --fix
// applies it.
func TestDoctorFixesAMissingFetchRefspec(t *testing.T) {
	ana, _ := sharedProject(t, "Payments via Stripe")
	gitIn(t, ana, "config", "--unset-all", "remote.origin.fetch", `^\+refs/beforecode/`)

	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0 with a failed check:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "fetch refspec", "missing for origin")
	wantFix := "fix: git config --add remote.origin.fetch '" + store.FetchRefspec + "'"
	if !strings.Contains(out, wantFix) {
		t.Errorf("output lacks %q:\n%s", wantFix, out)
	}

	out = mustBfc(t, ana, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "fetch refspec", store.FetchRefspec)
	if !slices.Contains(fetchRefspecs(t, ana), store.FetchRefspec) {
		t.Errorf("refspec not added: %v", fetchRefspecs(t, ana))
	}
	out = mustBfc(t, ana, "doctor")
	wantDoctor(t, out, "ok", "fetch refspec")
}

// joinedClones returns a clone that shared a plan, a second clone that
// joined with bfc bootstrap, and the bare remote.
func joinedClones(t *testing.T) (ana, ben, bare string) {
	t.Helper()
	ana, bare = sharedProject(t, "First plan")
	ben = plainClone(t, bare)
	mustBfc(t, ben, "bootstrap")
	return ana, ben, bare
}

// AC-1, AC-2, AC-3: a plans ref behind the remote fails with bfc pull as
// the fix, and --fix pulls.
func TestDoctorPullsWhenThePlansRefIsBehind(t *testing.T) {
	ana, ben, bare := joinedClones(t)
	mustBfc(t, ana, "new", "--no-edit", "Second plan")
	mustBfc(t, ana, "push")

	out, _ := bfcIn(t, ben, "doctor")
	wantDoctor(t, out, "FAIL", "in sync", "origin has commits this clone lacks")
	if !strings.Contains(out, "fix: bfc pull") {
		t.Errorf("output lacks the fix:\n%s", out)
	}
	gitIn(t, ben, "fetch", "-q", "origin")
	out, _ = bfcIn(t, ben, "doctor")
	wantDoctor(t, out, "FAIL", "in sync", "behind origin")

	out = mustBfc(t, ben, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "in sync", "up to date with origin")
	if got, want := gitIn(t, ben, "rev-parse", plansRef), gitIn(t, bare, "rev-parse", plansRef); got != want {
		t.Errorf("ben's plans ref = %s, want the remote's %s", got, want)
	}
}

// AC-3: on a diverged plans ref --fix merges; it neither pushes nor
// rewrites the ref.
func TestDoctorMergesADivergedPlansRefWithoutPushing(t *testing.T) {
	ana, ben, bare := joinedClones(t)
	mustBfc(t, ben, "new", "--no-edit", "Ben's plan")
	mustBfc(t, ana, "new", "--no-edit", "Ana's plan")
	mustBfc(t, ana, "push")
	gitIn(t, ben, "fetch", "-q", "origin")
	before := gitIn(t, ben, "rev-parse", plansRef)
	remote := gitIn(t, bare, "rev-parse", plansRef)

	out, _ := bfcIn(t, ben, "doctor")
	wantDoctor(t, out, "FAIL", "in sync", "diverged from origin")

	out = mustBfc(t, ben, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "in sync", "ahead of origin")
	after := gitIn(t, ben, "rev-parse", plansRef)
	for _, parent := range []string{before, remote} {
		if _, err := gitTry(ben, "merge-base", "--is-ancestor", parent, after); err != nil {
			t.Errorf("%s is not in ben's plans ref after --fix: the ref was rewritten", parent)
		}
	}
	if got := gitIn(t, bare, "rev-parse", plansRef); got != remote {
		t.Errorf("--fix changed the remote's plans ref from %s to %s", remote, got)
	}
}

// AC-3: a conflict stops the pull; nothing is committed and the check
// stays failed with the way out.
func TestDoctorFixLeavesAConflictToTheUser(t *testing.T) {
	ana, ben, _ := joinedClones(t)
	_, id := newPlan(t, ana, "Shared plan")
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")
	path := core.PlanDir(id) + "/intent.md"
	writeRef(t, ana, "ana edits", map[string][]byte{path: []byte("---\ntitle: Ana\n---\n")})
	writeRef(t, ben, "ben edits", map[string][]byte{path: []byte("---\ntitle: Ben\n---\n")})
	mustBfc(t, ana, "push")
	before := gitIn(t, ben, "rev-parse", plansRef)

	out, err := bfcIn(t, ben, "doctor", "--fix")
	if err == nil {
		t.Errorf("doctor --fix exited 0 over a conflict:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "in sync")
	if !strings.Contains(out, "fix failed:") || !strings.Contains(out, "bfc pull --keep local") {
		t.Errorf("output lacks the failed fix and the way out:\n%s", out)
	}
	if got := gitIn(t, ben, "rev-parse", plansRef); got != before {
		t.Error("a conflicting pull moved the plans ref")
	}
}

// AC-1, AC-3: a damaged index fails and --fix rebuilds it; one that is only
// behind the plans ref passes, because the next read updates it.
func TestDoctorRebuildsAnUnreadableIndex(t *testing.T) {
	ana, _ := sharedProject(t, "Payments via Stripe")
	mustBfc(t, ana, "list")
	mustBfc(t, ana, "new", "--no-edit", "Second plan")
	out := mustBfc(t, ana, "doctor")
	wantDoctor(t, out, "ok", "index", "behind")

	db := filepath.Join(gitIn(t, ana, "rev-parse", "--path-format=absolute", "--git-common-dir"), "beforecode", "index.db")
	writeFile(t, db, "this is not a database")
	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0 with a broken index:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "index", "cannot be read")
	if !strings.Contains(out, "fix: bfc doctor --fix") {
		t.Errorf("output lacks the fix:\n%s", out)
	}
	if got := readFile(t, db); got != "this is not a database" {
		t.Error("doctor without --fix changed the index")
	}

	out = mustBfc(t, ana, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "index", "readable and current")
	if out := mustBfc(t, ana, "list"); !strings.Contains(out, "Second plan") {
		t.Errorf("list after the rebuild:\n%s", out)
	}
}

// AC-5: every event file is validated; broken and unreadable ones are
// listed and the run goes on. A plan without a valid created event fails.
func TestDoctorListsInvalidEventFilesAndPlans(t *testing.T) {
	ana, _ := sharedProject(t, "Payments via Stripe")
	_, id := newPlan(t, ana, "Second plan")
	events := core.PlanDir(id) + "/events/"
	broken := events + "01JD3K8Q2X0000000000000001.yaml"
	lost := events + "01JD3K8Q2X0000000000000002.yaml"
	lostData := "type: withdrawn\n# this object goes missing\n"
	orphan := core.PlanDir(ulid.MustParse("01JD3K8Q2W00000000000000AA"))
	writeRef(t, ana, "damage", map[string][]byte{
		broken:                []byte("type: [\n"),
		lost:                  []byte(lostData),
		orphan + "/intent.md": []byte("---\ntitle: Orphan\n---\n"),
	})
	// Lose the object of one event file, as a broken clone would.
	blob := gitInStdin(t, ana, lostData, "hash-object", "--stdin")
	objects := gitIn(t, ana, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err := os.Remove(filepath.Join(objects, "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}

	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "event files", "2 of 4 not valid")
	wantDoctor(t, out, "FAIL", "plans", "1 of 3 without a valid created event")
	for _, want := range []string{broken + ": event", lost + ": cannot read object " + blob, "      " + orphan + "\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// The run went on to the last check.
	wantDoctor(t, out, "ok", "short IDs")
}

// ADR 0002 sections 4 and 8: skipped events and duplicate Short IDs are
// warnings; they do not fail the run.
func TestDoctorWarnsAboutSkippedEventsAndDuplicateShortIDs(t *testing.T) {
	ana, _ := sharedProject(t, "Payments via Stripe")
	short, id := newPlan(t, ana, "Second plan")
	// The plan author cannot approve their own plan.
	addEvent(t, ana, id, core.Event{Type: core.Approved, Author: core.Person{Name: "Ana", Email: "ana@example.com"}, PlanBlob: strings.Repeat("a", 40)})
	// A plan created offline elsewhere that got the same Short ID.
	twin := id
	twin[15] ^= 1
	created := core.Event{ID: ulid.MustParse("01JD3K8Q2X0000000000000003"), Type: core.Created, Plan: twin,
		Author: core.Person{Name: "Ben", Email: "ben@example.com"}, At: time.Now(), ShortID: short}
	writeRef(t, ana, "twin", map[string][]byte{
		core.PlanDir(twin) + "/intent.md":                               []byte("---\ntitle: Twin\n---\n"),
		core.PlanDir(twin) + "/events/" + created.ID.String() + ".yaml": core.EncodeEvent(created),
	})

	out := mustBfc(t, ana, "doctor")
	wantDoctor(t, out, "warn", "skipped events", "1 not allowed or out of order")
	if !strings.Contains(out, "(approved by ana@example.com)") {
		t.Errorf("output lacks the skipped event:\n%s", out)
	}
	wantDoctor(t, out, "warn", "short IDs", "1 used by more than one plan")
	if !strings.Contains(out, short+": ") {
		t.Errorf("output lacks the duplicate Short ID %s:\n%s", short, out)
	}
	wantDoctor(t, out, "ok", "event files")
	wantDoctor(t, out, "ok", "plans", "3 valid")
}

// In local mode the remote checks are skipped, not failed.
func TestDoctorSkipsTheRemoteChecksInLocalMode(t *testing.T) {
	dir := newLocalRepo(t)
	mustBfc(t, dir, "new", "--no-edit", "Solo plan")

	out := mustBfc(t, dir, "doctor")
	wantDoctor(t, out, "ok", "config", "local mode")
	for _, check := range []string{"fetch refspec", "remote", "in sync"} {
		wantDoctor(t, out, "skip", check, "local mode")
	}
	wantDoctor(t, out, "ok", "plans", "1 valid")
}

// AC-2: what fails before anything is set up says what to run.
func TestDoctorPointsAPlainCloneToBootstrap(t *testing.T) {
	_, bare := sharedProject(t, "Payments via Stripe")
	ben := plainClone(t, bare)

	out, err := bfcIn(t, ben, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0:\n%s", out)
	}
	wantDoctor(t, out, "ok", "config")
	wantDoctor(t, out, "FAIL", "plans ref", "does not exist")
	wantDoctor(t, out, "FAIL", "fetch refspec")
	wantDoctor(t, out, "ok", "remote", "origin has "+plansRef)
	wantDoctor(t, out, "skip", "format-version")
	if !strings.Contains(out, "fix: bfc bootstrap\n") {
		t.Errorf("output lacks the fix bfc bootstrap:\n%s", out)
	}
}

func TestDoctorWithoutConfigOrSharedPlans(t *testing.T) {
	ana, _ := newClone(t)
	mustBfc(t, ana, "init")
	if err := os.Remove(filepath.Join(ana, ".beforecode", "config.yaml")); err != nil {
		t.Fatal(err)
	}

	out, _ := bfcIn(t, ana, "doctor")
	wantDoctor(t, out, "FAIL", "config", "missing")
	wantDoctor(t, out, "FAIL", "remote", "origin has no "+plansRef+" yet")
	for _, want := range []string{"fix: bfc init, then commit .beforecode/config.yaml", "fix: bfc push"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestDoctorOutsideARepository(t *testing.T) {
	out, err := bfcIn(t, t.TempDir(), "doctor")
	if err == nil {
		t.Errorf("doctor exited 0:\n%s", out)
	}
	wantDoctor(t, out, "ok", "git version")
	wantDoctor(t, out, "FAIL", "repository", "not inside a git working tree")
	wantDoctor(t, out, "skip", "short IDs")
}

// AC-1, AC-2: a format-version this binary cannot use fails.
func TestDoctorChecksTheFormatVersion(t *testing.T) {
	dir := newLocalRepo(t)
	writeRef(t, dir, "from the future", map[string][]byte{"format-version": []byte("99\n")})
	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "format-version", "99 is newer than this bfc supports (1)")
	if !strings.Contains(out, "fix: update bfc") {
		t.Errorf("output lacks the fix:\n%s", out)
	}

	tree := gitIn(t, dir, "mktree") // an empty tree: a plans ref without format-version
	commit := gitIn(t, dir, "commit-tree", tree, "-p", plansRef, "-m", "lost format-version")
	gitIn(t, dir, "update-ref", plansRef, commit)
	out, _ = bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "format-version", "has no format-version file")
}
