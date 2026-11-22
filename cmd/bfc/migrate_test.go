package main

import (
	"strings"
	"testing"
)

// setFormatVersion rewrites format-version in a new commit to the plans
// ref, as a bfc of that version would have written it.
func setFormatVersion(t *testing.T, dir, version string) {
	t.Helper()
	writeRef(t, dir, "format-version "+version, map[string][]byte{"format-version": []byte(version + "\n")})
}

// Issue #100 AC-1: a v1 ref is read, but not written until it is migrated.
func TestAV1RefIsReadButNotWritten(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	mustBfc(t, dir, "new", "--no-edit", "Old plan")
	setFormatVersion(t, dir, "1")

	if out := mustBfc(t, dir, "list"); !strings.Contains(out, "Old plan") {
		t.Errorf("bfc list on a v1 ref:\n%s", out)
	}
	out, err := bfcIn(t, dir, "new", "--no-edit", "New plan")
	if err == nil {
		t.Fatalf("bfc new wrote to a v1 ref:\n%s", out)
	}
	if !strings.Contains(out, "format-version 1") || !strings.Contains(out, "bfc migrate") {
		t.Errorf("the refusal does not name bfc migrate:\n%s", out)
	}
}

// Issue #100 AC-1: a v3 ref is read with a warning and not written.
func TestANewerRefIsReadWithAWarning(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	mustBfc(t, dir, "new", "--no-edit", "Future plan")
	setFormatVersion(t, dir, "3")

	out := mustBfc(t, dir, "list")
	if !strings.Contains(out, "Future plan") || !strings.Contains(out, "warning: the plans ref has format-version 3") {
		t.Errorf("bfc list on a v3 ref:\n%s", out)
	}
	if out, err := bfcIn(t, dir, "migrate"); err == nil || !strings.Contains(out, "update bfc") {
		t.Errorf("bfc migrate on a v3 ref: err %v\n%s", err, out)
	}
}

// Issue #100 AC-2, AC-7: bfc migrate rewrites format-version in one commit,
// changes nothing else, does not push, and a second clone pulls the result.
func TestMigrateRewritesTheFormatVersion(t *testing.T) {
	t.Parallel()
	ana, ben, bare := twoClones(t)
	mustBfc(t, ana, "new", "--no-edit", "Payments")
	setFormatVersion(t, ana, "1")
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")
	before := gitIn(t, ana, "rev-parse", plansRef)
	remoteBefore := gitIn(t, bare, "rev-parse", plansRef)

	out := mustBfc(t, ana, "migrate")

	if !strings.Contains(out, "format-version 1 to 2") {
		t.Errorf("output:\n%s", out)
	}
	after := gitIn(t, ana, "rev-parse", plansRef)
	if parent := gitIn(t, ana, "rev-parse", plansRef+"^"); parent != before {
		t.Errorf("migrate made more than one commit: parent %s, want %s", parent, before)
	}
	if diff := gitIn(t, ana, "diff", "--name-only", before, after); diff != "format-version" {
		t.Errorf("migrate changed %q, want only format-version", diff)
	}
	if got := gitIn(t, ana, "show", plansRef+":format-version"); got != "2" {
		t.Errorf("format-version = %q", got)
	}
	if got := gitIn(t, bare, "rev-parse", plansRef); got != remoteBefore {
		t.Errorf("migrate pushed: remote %s, was %s", got, remoteBefore)
	}

	// Ben's v1 copy refuses to write until he pulls the migrated ref.
	if out, err := bfcIn(t, ben, "new", "--no-edit", "Refunds"); err == nil {
		t.Fatalf("bfc new wrote to Ben's v1 ref:\n%s", out)
	}
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")
	if got := gitIn(t, ben, "show", plansRef+":format-version"); got != "2" {
		t.Errorf("Ben's format-version = %q", got)
	}
	mustBfc(t, ben, "new", "--no-edit", "Refunds")
}

// Issue #100 AC-2: on a v2 ref there is nothing to do.
func TestMigrateOnAV2RefDoesNothing(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := mustBfc(t, dir, "migrate")

	if !strings.Contains(out, "nothing to do") {
		t.Errorf("output:\n%s", out)
	}
	if got := gitIn(t, dir, "rev-parse", plansRef); got != before {
		t.Errorf("migrate wrote a commit on a v2 ref")
	}
}

// Issue #100 AC-3: doctor fails a v1 ref with bfc migrate as the fix, and
// --fix does not migrate.
func TestDoctorSendsAV1RefToMigrate(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	setFormatVersion(t, dir, "1")

	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "FAIL", "format-version", "1 is older than this bfc writes (2)")
	if !strings.Contains(out, "fix: bfc migrate") {
		t.Errorf("output lacks the fix:\n%s", out)
	}
	out, _ = bfcIn(t, dir, "doctor", "--fix")
	wantDoctor(t, out, "FAIL", "format-version")
	if got := gitIn(t, dir, "show", plansRef+":format-version"); got != "1" {
		t.Errorf("doctor --fix migrated: format-version = %q", got)
	}
}
