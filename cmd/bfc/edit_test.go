package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

const validPlan = `## Context

Cards fail.

## Approach

Stripe.

## Affected areas

payments/

## Out of scope

Refunds.

## Tasks

- T-1: Add the client.

## Acceptance criteria

- AC-1: An invalid card returns 402.
`

// planCommits counts the commits on the plans ref.
func planCommits(t *testing.T, dir string) string {
	t.Helper()
	return gitIn(t, dir, "rev-list", "--count", plansRef)
}

func TestEditCreatesPlanMdFromTheTemplateAndCommitsTheSavedText(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	// The editor checks it got the template, then replaces it.
	fakeEditor(t, `grep -q '^## Acceptance criteria' "$1" || exit 3
cat > "$1" <<'PLAN'
`+validPlan+`PLAN`)

	out, err := bfcIn(t, dir, "edit", short)
	if err != nil {
		t.Fatalf("bfc edit: %v\n%s", err, out)
	}

	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/plan.md"); got != strings.TrimSpace(validPlan) {
		t.Errorf("plan.md in the ref =\n%s", got)
	}
	if msg := gitIn(t, dir, "log", "-1", "--format=%s", plansRef); msg != "bfc edit "+short+": plan.md" {
		t.Errorf("commit message = %q", msg)
	}
	if strings.Contains(out, "warning") {
		t.Errorf("valid plan produced warnings:\n%s", out)
	}
	if st := gitIn(t, dir, "status", "--porcelain"); st != "?? .beforecode/" {
		t.Errorf("working tree changed: %q", st)
	}
}

func TestEditIntent(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	fakeEditor(t, `printf 'More detail.\n' >> "$1"`)

	if out, err := bfcIn(t, dir, "edit", short, "--intent"); err != nil {
		t.Fatalf("bfc edit --intent: %v\n%s", err, out)
	}

	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/intent.md"); !strings.HasSuffix(got, "More detail.") || !strings.Contains(got, "title: Payments") {
		t.Errorf("intent.md =\n%s", got)
	}
	if msg := gitIn(t, dir, "log", "-1", "--format=%s", plansRef); msg != "bfc edit "+short+": intent.md" {
		t.Errorf("commit message = %q", msg)
	}
}

func TestEditUnchangedMakesNoCommit(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	before := planCommits(t, dir)
	fakeEditor(t, `true`)

	for _, args := range [][]string{{"edit", short}, {"edit", short, "--intent"}} {
		out, err := bfcIn(t, dir, args...)
		if err != nil {
			t.Fatalf("bfc %v: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, "no changes") {
			t.Errorf("bfc %v does not say nothing changed:\n%s", args, out)
		}
	}
	if got := planCommits(t, dir); got != before {
		t.Errorf("commits went from %s to %s", before, got)
	}
}

func TestEditKeepsTheUsersTextWhenTheDocumentChangedMeanwhile(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	// While "editing", someone else's change to plan.md lands in the ref.
	path := core.PlanDir(id) + "/plan.md"
	fakeEditor(t, `blob=$(printf 'their version\n' | git hash-object -w --stdin)
export GIT_INDEX_FILE="$1.index"
git read-tree `+plansRef+`
git update-index --cacheinfo 100644,$blob,`+path+`
c=$(git commit-tree $(git write-tree) -p `+plansRef+` -m theirs)
git update-ref `+plansRef+` $c
printf 'my precious words\n' >> "$1"`)

	out, err := bfcIn(t, dir, "edit", short)

	if err == nil {
		t.Fatalf("bfc edit succeeded over a concurrent change:\n%s", out)
	}
	if got := gitIn(t, dir, "show", plansRef+":"+path); got != "their version" {
		t.Errorf("their change was overwritten: %q", got)
	}
	kept := regexp.MustCompile(`\S+\.md`).FindAllString(out, -1)
	found := false
	for _, p := range kept {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), "my precious words") {
			found = true
		}
	}
	if !found || !strings.Contains(out, "changed") {
		t.Errorf("output does not report the conflict and where the text is kept:\n%s", out)
	}
}

func TestEditingAnApprovedPlanReturnsItToReviewWithoutAnEvent(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	blob := planBlob(t, dir, id)
	addEvent(t, dir, id, core.Event{Type: core.Submitted, Author: adaP, PlanBlob: blob, RequiredApprovals: 1})
	addEvent(t, dir, id, core.Event{Type: core.Approved, Author: janaP, PlanBlob: blob})
	if out, _ := bfcIn(t, dir, "show", short); !strings.Contains(out, "State:      approved") {
		t.Fatalf("setup: plan is not approved:\n%s", out)
	}
	events := gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(id)+"/events")
	fakeEditor(t, `printf -- '- AC-2: A refund is possible.\n' >> "$1"`)

	out, err := bfcIn(t, dir, "edit", short)
	if err != nil {
		t.Fatalf("bfc edit: %v\n%s", err, out)
	}

	if !strings.Contains(out, "in-review") || !strings.Contains(out, "approvals") {
		t.Errorf("output does not say the plan is back in review:\n%s", out)
	}
	if got := gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(id)+"/events"); got != events {
		t.Errorf("an event was written:\n%s", got)
	}
	if out, _ := bfcIn(t, dir, "show", short); !strings.Contains(out, "State:      in-review") || !strings.Contains(out, "Approvals:  0/1") {
		t.Errorf("bfc show after the edit:\n%s", out)
	}
}

func TestEditWarnsAboutPlanProblemsButCommits(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	fakeEditor(t, `printf '## Context\n\n## Acceptance criteria\n\n- AC-1: a\n- AC-1: b\n' > "$1"`)

	out, err := bfcIn(t, dir, "edit", short)
	if err != nil {
		t.Fatalf("bfc edit: %v\n%s", err, out)
	}

	for _, want := range []string{"warning", `missing section "## Approach"`, "AC-1 is used more than once"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if got := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/plan.md"); !strings.Contains(got, "- AC-1: b") {
		t.Errorf("plan.md was not committed:\n%s", got)
	}
}

func TestEditIntentWithoutTitleKeepsTheText(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	before := planCommits(t, dir)
	fakeEditor(t, `printf -- '---\ntitle: \n---\n\nkeep me\n' > "$1"`)

	out, err := bfcIn(t, dir, "edit", short, "--intent")

	if err == nil || !strings.Contains(out, "title") || !strings.Contains(out, "kept in") {
		t.Errorf("bfc edit --intent without title: %v\n%s", err, out)
	}
	if got := planCommits(t, dir); got != before {
		t.Error("a commit was made")
	}
}

func TestEditRefusesAClosedPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	addEvent(t, dir, id, core.Event{Type: core.Closed, Author: adaP})
	fakeEditor(t, `echo "editor must not run" >&2; exit 1`)

	out, err := bfcIn(t, dir, "edit", short)

	if err == nil || !strings.Contains(out, "closed") {
		t.Errorf("bfc edit of a closed plan: %v\n%s", err, out)
	}
}

func TestEditUnknownPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)

	out, err := bfcIn(t, dir, "edit", "bfc-zzzz")
	if err == nil || !strings.Contains(out, "no plan") {
		t.Errorf("bfc edit of an unknown plan: %v\n%s", err, out)
	}
}

func TestEditingAPlanInReviewWithSomeApprovalsSaysNothingAboutReturning(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	blob := planBlob(t, dir, id)
	addEvent(t, dir, id, core.Event{Type: core.Submitted, Author: adaP, PlanBlob: blob, RequiredApprovals: 2})
	addEvent(t, dir, id, core.Event{Type: core.Approved, Author: janaP, PlanBlob: blob})
	fakeEditor(t, `printf -- '- AC-2: More.\n' >> "$1"`)

	out, err := bfcIn(t, dir, "edit", short)
	if err != nil {
		t.Fatalf("bfc edit: %v\n%s", err, out)
	}
	if strings.Contains(out, "back in-review") {
		t.Errorf("plan that never left review is said to be back:\n%s", out)
	}
}
