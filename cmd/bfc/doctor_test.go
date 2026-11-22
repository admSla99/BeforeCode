package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")

	out := mustBfc(t, ana, "doctor")
	for _, check := range []string{"git version", "repository", "config", "plans ref", "fetch refspec", "remote", "objects",
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
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")
	gitIn(t, ana, "config", "--unset-all", "remote.origin.fetch", `^\+refs/beforecode/`)

	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0 with a failed check:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "fetch refspec", "missing for origin")
	wantFix := "fix: bfc doctor --fix: adds it; or git config --add remote.origin.fetch '" + store.FetchRefspec + "'"
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
	dir := copyFixture(t, "joined", func(dir string) error {
		if err := buildSharedProject(dir, "First plan"); err != nil {
			return err
		}
		if err := buildBenClone(dir); err != nil {
			return err
		}
		return bfcSteps([]string{filepath.Join(dir, "ben"), "bootstrap"})
	})
	return filepath.Join(dir, "work"), filepath.Join(dir, "ben"), filepath.Join(dir, "remote.git")
}

// AC-1, AC-2, AC-3: a plans ref behind the remote fails with bfc pull as
// the fix, and --fix pulls.
func TestDoctorPullsWhenThePlansRefIsBehind(t *testing.T) {
	t.Parallel()
	ana, ben, bare := joinedClones(t)
	mustBfc(t, ana, "new", "--no-edit", "Second plan")
	mustBfc(t, ana, "push")

	out, _ := bfcIn(t, ben, "doctor")
	wantDoctor(t, out, "FAIL", "in sync", "origin has commits this clone lacks")
	if !strings.Contains(out, "fix: bfc doctor --fix: pulls, with a fast-forward or a merge commit; or bfc pull\n") {
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
	t.Parallel()
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
	t.Parallel()
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
	if !strings.Contains(out, "fix failed:") || !strings.Contains(out, "fix: bfc pull --keep local") {
		t.Errorf("output lacks the failed fix and the way out:\n%s", out)
	}
	if got := gitIn(t, ben, "rev-parse", plansRef); got != before {
		t.Error("a conflicting pull moved the plans ref")
	}
}

// AC-1, AC-3: a damaged index fails and --fix rebuilds it; one that is only
// behind the plans ref passes, because the next read updates it.
func TestDoctorRebuildsAnUnreadableIndex(t *testing.T) {
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")
	mustBfc(t, ana, "list")
	mustBfc(t, ana, "new", "--no-edit", "Second plan")
	out := mustBfc(t, ana, "doctor")
	wantDoctor(t, out, "ok", "index", "the next read updates it")

	db := filepath.Join(gitIn(t, ana, "rev-parse", "--path-format=absolute", "--git-common-dir"), "beforecode", "index.db")
	writeFile(t, db, "this is not a database")
	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0 with a broken index:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "index", "cannot be read")
	if !strings.Contains(out, "fix: bfc doctor --fix: rebuilds it from the plans ref\n") {
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
	t.Parallel()
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
		// Readers ignore unknown entries (ADR 0002 section 1).
		events + "README":        []byte("notes\n"),
		events + "drafts/x.yaml": []byte("type: [\n"),
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
	if strings.Contains(out, "README") || strings.Contains(out, "drafts") {
		t.Errorf("unknown entries in events/ are listed:\n%s", out)
	}
	// The run went on to the last check.
	wantDoctor(t, out, "ok", "short IDs")

	// Issue #48 AC-4: --fix leaves broken event files and invalid plans
	// as they are; it only reports them.
	before := gitIn(t, ana, "rev-parse", plansRef)
	out, _ = bfcIn(t, ana, "doctor", "--fix")
	wantDoctor(t, out, "FAIL", "event files", "2 of 4 not valid")
	wantDoctor(t, out, "FAIL", "plans", "1 of 3 without a valid created event")
	if got := gitIn(t, ana, "rev-parse", plansRef); got != before {
		t.Errorf("--fix moved the plans ref over broken event files:\n%s", gitIn(t, ana, "log", "--stat", before+".."+got, plansRef))
	}
}

// ADR 0002 sections 4 and 8: skipped events and duplicate Short IDs are
// warnings; they do not fail the run.
func TestDoctorWarnsAboutSkippedEventsAndDuplicateShortIDs(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	dir := newLocalRepo(t)
	writeRef(t, dir, "from the future", map[string][]byte{"format-version": []byte("99\n")})
	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "format-version", "99 is newer than this bfc supports (2)")
	if !strings.Contains(out, "fix: update bfc") {
		t.Errorf("output lacks the fix:\n%s", out)
	}

	tree := gitIn(t, dir, "mktree") // an empty tree: a plans ref without format-version
	commit := gitIn(t, dir, "commit-tree", tree, "-p", plansRef, "-m", "lost format-version")
	gitIn(t, dir, "update-ref", plansRef, commit)
	out, _ = bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "format-version", "has no format-version file")

	// Issue #48 Q1: also a plans ref without plans gets one, as after an
	// interrupted bfc init.
	out = mustBfc(t, dir, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "format-version", "2")
}

// Issue #48 AC-5: a repair that fails still leaves a fix line that says
// what to do.
func TestDoctorFixLineAfterAFailedRepair(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	mustBfc(t, dir, "new", "--no-edit", "Solo plan")
	dropFromRef(t, dir, "format-version")
	// Another writer holds the plans ref, so the commit cannot be made.
	lock := filepath.Join(gitIn(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"), "refs", "beforecode", "plans.lock")
	writeFile(t, lock, "")

	out, _ := bfcIn(t, dir, "doctor", "--fix")
	wantDoctor(t, out, "FAIL", "format-version")
	if !strings.Contains(out, "fix failed:") || !strings.Contains(out, "fix: bfc doctor --fix: adds format-version 2") {
		t.Errorf("output lacks the failed fix and what to do:\n%s", out)
	}
}

// dropFromRef commits the plans ref without the file at the root named
// name, as a damaged clone might have it.
func dropFromRef(t *testing.T, dir, name string) {
	t.Helper()
	var keep []string
	for _, line := range strings.Split(gitIn(t, dir, "ls-tree", plansRef), "\n") {
		if !strings.HasSuffix(line, "\t"+name) {
			keep = append(keep, line)
		}
	}
	tree := gitInStdin(t, dir, strings.Join(keep, "\n")+"\n", "mktree")
	commit := gitIn(t, dir, "commit-tree", tree, "-p", plansRef, "-m", "lose "+name)
	gitIn(t, dir, "update-ref", plansRef, commit)
}

// Issue #48 AC-1, AC-3, AC-5: a plans ref with plans but no format-version
// gets one in a new commit on top of the tip; nothing else is touched.
func TestDoctorFixAddsAMissingFormatVersion(t *testing.T) {
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")
	dropFromRef(t, ana, "format-version")
	head := gitIn(t, ana, "rev-parse", "HEAD")
	status := gitIn(t, ana, "status", "--porcelain")

	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "format-version", "has no format-version file")
	for _, want := range []string{"fix: bfc doctor --fix: adds format-version 2 in a new commit",
		"1 failed check can be repaired with bfc doctor --fix"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	before := gitIn(t, ana, "rev-parse", plansRef)

	out = mustBfc(t, ana, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "format-version", "2")
	if got := gitIn(t, ana, "rev-parse", plansRef+"^"); got != before {
		t.Errorf("the repair is not one new commit on top of %s (parent %s)", before, got)
	}
	if msg := gitIn(t, ana, "log", "-1", "--format=%s", plansRef); msg != "bfc doctor: add format-version 2" {
		t.Errorf("commit message %q", msg)
	}
	if got := gitIn(t, ana, "show", plansRef+":format-version"); got != "2" {
		t.Errorf("format-version = %q", got)
	}
	if gitIn(t, ana, "rev-parse", "HEAD") != head || gitIn(t, ana, "status", "--porcelain") != status {
		t.Error("the repair touched the current branch, the index or the working tree")
	}
	out = mustBfc(t, ana, "doctor")
	wantDoctor(t, out, "ok", "format-version", "2")
	if out := mustBfc(t, ana, "list"); !strings.Contains(out, "Payments via Stripe") {
		t.Errorf("list after the repair:\n%s", out)
	}
}

// Issue #48 Q1: a ref with an event type this bfc does not know may be of
// another format version, so --fix does not guess it.
func TestDoctorDoesNotAddAFormatVersionOverUnknownEventTypes(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	_, id := newPlan(t, dir, "Solo plan")
	unknown := core.PlanDir(id) + "/events/01JD3K8Q2X0000000000000009.yaml"
	writeRef(t, dir, "from the future", map[string][]byte{unknown: []byte("type: archived\nplan: " + id.String() +
		"\nauthor: {name: Ana, email: ana@example.com}\nat: 2026-11-16T21:30:00Z\n")})
	dropFromRef(t, dir, "format-version")
	before := gitIn(t, dir, "rev-parse", plansRef)

	out, _ := bfcIn(t, dir, "doctor", "--fix")
	wantDoctor(t, out, "FAIL", "format-version", "has no format-version file")
	for _, want := range []string{unknown + " (archived)", "fix: update bfc"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if gitIn(t, dir, "rev-parse", plansRef) != before {
		t.Error("--fix wrote to the plans ref")
	}
}

// loseObject removes the loose object of the blob with this content from
// the clone, as a broken clone would have lost it, and returns its ID.
func loseObject(t *testing.T, dir, content string) string {
	t.Helper()
	blob := gitInStdin(t, dir, content, "hash-object", "--stdin")
	objects := gitIn(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err := os.Remove(filepath.Join(objects, "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	return blob
}

// Issue #48 AC-2, AC-3, AC-5: objects the plans ref needs and this clone
// lacks are fetched again from the sync remote, without a new commit.
func TestDoctorFixFetchesMissingObjectsAgain(t *testing.T) {
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")
	_, id := newPlan(t, ana, "Second plan")
	mustBfc(t, ana, "push")
	path := core.PlanDir(id) + "/intent.md"
	blob := loseObject(t, ana, gitIn(t, ana, "show", plansRef+":"+path)+"\n")
	before := gitIn(t, ana, "rev-parse", plansRef)

	out, err := bfcIn(t, ana, "doctor")
	if err == nil {
		t.Errorf("doctor exited 0:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "objects", "1 missing from this clone")
	for _, want := range []string{"      " + path + " (" + blob + ")\n",
		"fix: bfc doctor --fix: fetches them again from origin"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	out = mustBfc(t, ana, "doctor", "--fix")
	wantDoctor(t, out, "fixed", "objects", "all present")
	if _, err := gitTry(ana, "cat-file", "-e", blob); err != nil {
		t.Errorf("object %s is still missing", blob)
	}
	if got := gitIn(t, ana, "rev-parse", plansRef); got != before {
		t.Error("fetching objects again moved the plans ref")
	}
	wantDoctor(t, mustBfc(t, ana, "doctor"), "ok", "objects")
}

// Issue #48 AC-2: an object the remote does not have either is lost, and
// bfc doctor says which file it was and what to do next.
func TestDoctorListsObjectsTheRemoteLacksAsLost(t *testing.T) {
	t.Parallel()
	ana, _ := sharedProject(t, "Payments via Stripe")
	_, id := newPlan(t, ana, "Never pushed")
	path := core.PlanDir(id) + "/intent.md"
	blob := loseObject(t, ana, gitIn(t, ana, "show", plansRef+":"+path)+"\n")

	out, err := bfcIn(t, ana, "doctor", "--fix")
	if err == nil {
		t.Errorf("doctor --fix exited 0 with a lost object:\n%s", out)
	}
	wantDoctor(t, out, "FAIL", "objects", "1 missing here and on origin: lost")
	for _, want := range []string{"      " + path + " (" + blob + ")\n", "fix: bfc cannot restore them"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// Issue #48 Q3: in local mode there is no remote to fetch from.
func TestDoctorHasNoRepairForMissingObjectsInLocalMode(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	_, id := newPlan(t, dir, "Solo plan")
	path := core.PlanDir(id) + "/intent.md"
	loseObject(t, dir, gitIn(t, dir, "show", plansRef+":"+path)+"\n")

	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "objects", "1 missing from this clone; local mode has no remote to fetch them from: lost")
	if strings.Contains(out, "bfc doctor --fix") {
		t.Errorf("a failure without a repair offers --fix:\n%s", out)
	}
}

// Issue #59, AC-6: bfc doctor says whether claude.command is found. Claude
// Code is optional, so a missing one is a warning that does not fail the run.
func TestDoctorReportsWhetherClaudeCodeIsFound(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	cfg := filepath.Join(repo, ".beforecode", "config.yaml")
	withCommand := func(command string) {
		writeFile(t, cfg, "version: 3\napproval:\n  required_approvals: 1\nsync:\n  enabled: false\nclaude:\n  command: '"+command+"'\n")
	}

	missing := filepath.Join(t.TempDir(), "no-claude")
	withCommand(missing)
	out := mustBfc(t, repo, "doctor")
	wantDoctor(t, out, "warn", "claude", missing, "not found", "claude.command")

	found := filepath.Join(t.TempDir(), "my-claude")
	if runtime.GOOS == "windows" {
		found += ".exe"
	}
	if err := os.WriteFile(found, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	withCommand(found)
	wantDoctor(t, mustBfc(t, repo, "doctor"), "ok", "claude", found)
}

// Issue #61, AC-7: bfc doctor warns when .claude/settings.json runs
// bfc hook pre-tool-use but bfc is not on PATH: Claude Code then cannot run
// the hook and edits go unchecked.
func TestDoctorWarnsWhenTheHookIsInstalledButBfcIsNotOnPath(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	setEnv(t, "PATH", bin+string(os.PathListSeparator)+filepath.Dir(git))

	wantDoctor(t, mustBfc(t, repo, "doctor"), "skip", "claude hook", ".claude/settings.json")

	writeFile(t, filepath.Join(repo, ".claude", "settings.json"), `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit|Write|MultiEdit|NotebookEdit", "hooks": [{"type": "command", "command": "bfc hook pre-tool-use"}]}
    ]
  }
}`)
	// mustBfc: a warning does not make bfc doctor fail.
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "claude hook", ".claude/settings.json", "bfc hook pre-tool-use", "bfc is not on PATH")

	exe := filepath.Join(bin, "bfc")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	wantDoctor(t, mustBfc(t, repo, "doctor"), "ok", "claude hook", exe)

	// A quoted path, with a space, as on Windows.
	spaced := filepath.Join(t.TempDir(), "Program Files", filepath.Base(exe))
	command, _ := json.Marshal(`"` + spaced + `" hook pre-tool-use`)
	writeFile(t, filepath.Join(repo, ".claude", "settings.json"),
		`{"hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": `+string(command)+`}]}]}}`)
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "claude hook", spaced, "is not found")
}

// Issue #63, AC-5: bfc doctor reports whether .claude/settings.json has the
// bfc hook and whether the beforecode skill is this bfc's version of it.
func TestDoctorReportsTheClaudeSetup(t *testing.T) {
	t.Parallel()
	repo := newLocalRepo(t)

	out := mustBfc(t, repo, "doctor")
	wantDoctor(t, out, "skip", "claude hook", "no .claude/settings.json", "bfc setup claude")
	wantDoctor(t, out, "skip", "claude skill", "bfc setup claude")

	writeFile(t, filepath.Join(repo, ".claude", "settings.json"), `{"model": "opus"}`)
	out = mustBfc(t, repo, "doctor")
	wantDoctor(t, out, "warn", "claude hook", "does not run bfc hook pre-tool-use", "bfc setup claude")
	wantDoctor(t, out, "warn", "claude skill", ".claude/skills/beforecode/SKILL.md", "missing", "bfc setup claude")

	writeFile(t, filepath.Join(repo, ".claude", "settings.json"),
		`{"hooks": {"PreToolUse": [{"matcher": "Edit", "hooks": [{"type": "command", "command": "bfc hook pre-tool-use"}]}]}}`)
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "claude hook", "only for Edit", "bfc setup claude")

	mustBfc(t, repo, "setup", "claude")
	out = mustBfc(t, repo, "doctor")
	wantDoctor(t, out, "ok", "claude skill", "matches this bfc")

	writeFile(t, filepath.Join(repo, ".claude", "skills", "beforecode", "SKILL.md"), "an older skill\n")
	wantDoctor(t, mustBfc(t, repo, "doctor"), "warn", "claude skill", "differs from this bfc", "bfc setup claude")
}
