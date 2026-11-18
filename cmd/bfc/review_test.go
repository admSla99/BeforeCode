package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// as makes dir's git (and so bfc) act as the given person.
func as(t *testing.T, dir string, p core.Person) {
	t.Helper()
	gitIn(t, dir, "config", "user.name", p.Name)
	gitIn(t, dir, "config", "user.email", p.Email)
}

var benP = core.Person{Name: "Ben Bitdiddle", Email: "ben@example.com"}

// requireApprovals sets approval.required_approvals in the config file.
func requireApprovals(t *testing.T, dir, n string) {
	t.Helper()
	path := filepath.Join(dir, ".beforecode", "config.yaml")
	cfg := readFile(t, path)
	i := strings.Index(cfg, "required_approvals: ")
	end := i + strings.Index(cfg[i:], "\n")
	writeFile(t, path, cfg[:i]+"required_approvals: "+n+cfg[end:])
}

// draftWithPlan creates a plan by ada with a valid plan.md.
func draftWithPlan(t *testing.T) (dir, short string, id ulid.ULID) {
	t.Helper()
	dir = newLocalRepo(t)
	short, id = newPlan(t, dir, "Payments")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	return dir, short, id
}

// events returns the events of a plan, oldest first.
func events(t *testing.T, dir string, id ulid.ULID) []core.Event {
	t.Helper()
	var out []core.Event
	for _, name := range strings.Fields(gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(id)+"/events")) {
		eid := ulid.MustParseStrict(strings.TrimSuffix(name, ".yaml"))
		e, err := core.ParseEvent(eid, []byte(gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/events/"+name)))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func last(t *testing.T, dir string, id ulid.ULID) core.Event {
	t.Helper()
	es := events(t, dir, id)
	return es[len(es)-1]
}

func state(t *testing.T, dir, short string) string {
	t.Helper()
	for _, line := range strings.Split(mustBfc(t, dir, "show", short), "\n") {
		if s, ok := strings.CutPrefix(line, "State:"); ok {
			return strings.TrimSpace(s)
		}
	}
	t.Fatal("no State line")
	return ""
}

// AC-1 and AC-2: submit, then another person approves the exact plan.md.
func TestSubmitThenApprove(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)

	out := mustBfc(t, dir, "submit", short)
	if !strings.Contains(out, short+"  in-review") {
		t.Errorf("submit: %s", out)
	}
	e := last(t, dir, id)
	if e.Type != core.Submitted || e.RequiredApprovals != 1 || e.PlanBlob != planBlob(t, dir, id) || e.Author.Email != adaP.Email {
		t.Errorf("submitted event = %+v", e)
	}

	as(t, dir, janaP)
	out = mustBfc(t, dir, "approve", short)
	if !strings.Contains(out, short+"  approved") || !strings.Contains(out, "1 of 1") {
		t.Errorf("approve: %s", out)
	}
	e = last(t, dir, id)
	if e.Type != core.Approved || e.PlanBlob != planBlob(t, dir, id) || e.Author.Email != janaP.Email {
		t.Errorf("approved event = %+v", e)
	}
	if got := state(t, dir, short); got != "approved" {
		t.Errorf("state = %s", got)
	}
}

// AC-1: submit needs a plan.md that passes the checks.
func TestSubmitRefusesAMissingOrIncompletePlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	if out, err := bfcIn(t, dir, "submit", short); err == nil || !strings.Contains(out, "no plan.md") {
		t.Errorf("without plan.md: %v\n%s", err, out)
	}
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte("## Context\n\n## Acceptance criteria\n\n- AC-1:\n")})
	before := planCommits(t, dir)
	out, err := bfcIn(t, dir, "submit", short)
	if err == nil || !strings.Contains(out, `missing section "## Approach"`) || !strings.Contains(out, "AC-1 has no text") {
		t.Errorf("incomplete plan.md: %v\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Error("a refused submit wrote to the plans ref")
	}
}

// AC-3
func TestTheAuthorCannotApproveOrSubmitOthersCannotSubmit(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	as(t, dir, janaP)
	if out, err := bfcIn(t, dir, "submit", short); err == nil || !strings.Contains(out, "only the plan author") {
		t.Errorf("submit by jana: %v\n%s", err, out)
	}
	as(t, dir, adaP)
	mustBfc(t, dir, "submit", short)
	before := planCommits(t, dir)
	// The same email in other letter case is the same person.
	as(t, dir, core.Person{Name: "Ada", Email: "ADA@example.com"})
	out, err := bfcIn(t, dir, "approve", short)
	if err == nil || !strings.Contains(out, "your own plan") {
		t.Errorf("approve by the author: %v\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Error("a refused approve wrote to the plans ref")
	}
}

// AC-4
func TestRequestChangesWritesAMessageAndReturnsToDraft(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)

	as(t, dir, benP)
	if out, err := bfcIn(t, dir, "request-changes", short); err == nil {
		t.Errorf("request-changes without -m succeeded:\n%s", out)
	}
	out := mustBfc(t, dir, "request-changes", short, "-m", "Split AC-1 in two.")
	if !strings.Contains(out, short+"  draft") {
		t.Errorf("request-changes: %s", out)
	}
	e := last(t, dir, id)
	if e.Type != core.ChangesRequested || e.Author.Email != benP.Email || e.PlanBlob != planBlob(t, dir, id) {
		t.Fatalf("event = %+v", e)
	}
	msg := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(id)+"/messages/"+e.Message+".md")
	if !strings.Contains(msg, "email: ben@example.com") || !strings.HasSuffix(msg, "Split AC-1 in two.") {
		t.Errorf("message file:\n%s", msg)
	}
	if got := state(t, dir, short); got != "draft" {
		t.Errorf("state = %s", got)
	}

	as(t, dir, adaP)
	mustBfc(t, dir, "submit", short)
	if out, err := bfcIn(t, dir, "request-changes", short, "-m", "x"); err == nil || !strings.Contains(out, "bfc withdraw") {
		t.Errorf("request-changes by the author: %v\n%s", err, out)
	}
}

// AC-5: an Approval of an older plan.md does not count.
func TestAnApprovalOfAnOlderPlanDoesNotCount(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)

	writeRef(t, dir, "edit", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan + "- AC-2: Also this.\n")})
	if got := state(t, dir, short); got != "in-review" {
		t.Errorf("state after the edit = %s, want in-review", got)
	}
	out := mustBfc(t, dir, "approve", short)
	if !strings.Contains(out, "approved") {
		t.Errorf("approving the new version: %s", out)
	}
}

// AC-6: commands that do not fit the state name it.
func TestCommandsInTheWrongStateNameIt(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	as(t, dir, janaP)
	for _, args := range [][]string{{"approve", short}, {"request-changes", short, "-m", "x"}} {
		if out, err := bfcIn(t, dir, args...); err == nil || !strings.Contains(out, short+" is draft") {
			t.Errorf("bfc %v on a draft: %v\n%s", args, err, out)
		}
	}
	as(t, dir, adaP)
	mustBfc(t, dir, "submit", short)
	if out, err := bfcIn(t, dir, "submit", short); err == nil || !strings.Contains(out, short+" is in-review") {
		t.Errorf("second submit: %v\n%s", err, out)
	}
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)
	as(t, dir, benP)
	if out, err := bfcIn(t, dir, "approve", short); err == nil || !strings.Contains(out, short+" is approved") {
		t.Errorf("approve when approved: %v\n%s", err, out)
	}
}

// AC-7: the policy comes from config.yaml at submit time.
func TestTheApprovalPolicyComesFromTheConfigAtSubmit(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	requireApprovals(t, dir, "2")
	out := mustBfc(t, dir, "submit", short)
	if !strings.Contains(out, "needs 2 approvals") || last(t, dir, id).RequiredApprovals != 2 {
		t.Errorf("submit: %s", out)
	}

	as(t, dir, janaP)
	if out := mustBfc(t, dir, "approve", short); !strings.Contains(out, short+"  in-review") || !strings.Contains(out, "1 of 2") {
		t.Errorf("first approve: %s", out)
	}
	if out, err := bfcIn(t, dir, "approve", short); err == nil || !strings.Contains(out, "already approved") {
		t.Errorf("second approve by jana: %v\n%s", err, out)
	}
	// Lowering the policy later does not change a plan already in review.
	requireApprovals(t, dir, "1")
	if got := state(t, dir, short); got != "in-review" {
		t.Errorf("state after lowering the policy = %s", got)
	}
	as(t, dir, benP)
	if out := mustBfc(t, dir, "approve", short); !strings.Contains(out, short+"  approved") || !strings.Contains(out, "2 of 2") {
		t.Errorf("second approve: %s", out)
	}
}

func TestSubmitWithoutAConfigFileNeedsOneApproval(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	if err := os.Remove(filepath.Join(dir, ".beforecode", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	mustBfc(t, dir, "submit", short)
	if n := last(t, dir, id).RequiredApprovals; n != 1 {
		t.Errorf("required approvals = %d, want 1", n)
	}
}

// AC-1 and AC-4: withdrawing ends the round; a new submit needs new
// Approvals.
func TestWithdrawReturnsToDraftAndEndsTheRound(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, janaP)
	mustBfc(t, dir, "approve", short)

	as(t, dir, adaP)
	out := mustBfc(t, dir, "withdraw", short)
	if !strings.Contains(out, short+"  draft (withdrawn)") {
		t.Errorf("withdraw: %s", out)
	}
	if e := last(t, dir, id); e.Type != core.Withdrawn || e.Author.Email != adaP.Email || e.PlanBlob != "" {
		t.Errorf("event = %+v", e)
	}
	if got := state(t, dir, short); got != "draft" {
		t.Errorf("state = %s", got)
	}

	// plan.md is unchanged, yet jana's Approval from the ended round does
	// not carry over.
	out = mustBfc(t, dir, "submit", short)
	if !strings.Contains(out, short+"  in-review") {
		t.Errorf("resubmit: %s", out)
	}
	as(t, dir, janaP)
	if out := mustBfc(t, dir, "approve", short); !strings.Contains(out, "approved (1 of 1") {
		t.Errorf("approve in the new round: %s", out)
	}
}

func TestWithdrawFromInReview(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	if out := mustBfc(t, dir, "withdraw", short); !strings.Contains(out, "draft") {
		t.Errorf("withdraw: %s", out)
	}
}

// AC-2 and AC-3
func TestWithdrawIsForTheAuthorAndOnlyInReviewOrApproved(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	if out, err := bfcIn(t, dir, "withdraw", short); err == nil || !strings.Contains(out, short+" is draft") {
		t.Errorf("withdraw a draft: %v\n%s", err, out)
	}
	mustBfc(t, dir, "submit", short)
	before := planCommits(t, dir)
	as(t, dir, janaP)
	if out, err := bfcIn(t, dir, "withdraw", short); err == nil || !strings.Contains(out, "only the plan author") {
		t.Errorf("withdraw by jana: %v\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Error("a refused withdraw wrote to the plans ref")
	}
}

// AC-5: an Approval written concurrently, after the withdrawal in ULID
// order, is skipped.
func TestAConcurrentApprovalAfterWithdrawIsSkipped(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	mustBfc(t, dir, "withdraw", short)
	// jana approved the version she saw before the withdrawal reached her.
	addEvent(t, dir, id, core.Event{Type: core.Approved, Author: janaP, PlanBlob: planBlob(t, dir, id)})
	if got := state(t, dir, short); got != "draft" {
		t.Errorf("state = %s, want draft", got)
	}
	mustBfc(t, dir, "submit", short)
	if got := state(t, dir, short); got != "in-review" {
		t.Errorf("state after resubmit = %s, want in-review (the skipped Approval must not count)", got)
	}
}

// Each review command names itself in the commit message on the plans ref.
func TestReviewCommitsAreNamedAfterTheCommand(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	subject := func() string { return gitIn(t, dir, "log", "-1", "--format=%s", plansRef) }
	steps := []struct {
		who  core.Person
		args []string
	}{
		{adaP, []string{"submit", short}},
		{janaP, []string{"approve", short}},
		{benP, []string{"request-changes", short, "-m", "x"}},
		{adaP, []string{"submit", short}},
		{adaP, []string{"withdraw", short}},
	}
	for _, step := range steps {
		as(t, dir, step.who)
		mustBfc(t, dir, step.args...)
		if got, want := subject(), "bfc "+step.args[0]+" "+short; got != want {
			t.Errorf("commit message = %q, want %q", got, want)
		}
	}
}
