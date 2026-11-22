package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

var (
	checkPlanID = ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	bot         = core.Person{Name: "github-actions[bot]", Email: "41898282+github-actions[bot]@users.noreply.github.com"}
)

// The check file of ADR 0013 section 6, as bfc writes it.
const checkFile = `---
author:
  name: github-actions[bot]
  email: 41898282+github-actions[bot]@users.noreply.github.com
at: 2026-11-22T11:00:00Z
agent: claude-code
plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D
pr: 42
head_sha: 9f2c
base_sha: 1a7e
plan_blob: 3b18e512dba79e4c8300dd08aeb37f8e728b8dad
criteria:
  AC-2: met
  AC-10: not-met
  AC-3: cannot-verify
out_of_scope:
  - Changes the retry policy of the email worker.
---

Two of three met.
`

func sampleCheck() core.Check {
	return core.Check{
		ID: ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E"), Author: bot,
		At: time.Date(2026, 11, 22, 11, 0, 0, 0, time.UTC), Agent: "claude-code",
		Plan: checkPlanID, PR: 42, HeadSHA: "9f2c", BaseSHA: "1a7e",
		PlanBlob: "3b18e512dba79e4c8300dd08aeb37f8e728b8dad",
		Criteria: []core.CriterionResult{
			{ID: "AC-2", Status: core.Met}, {ID: "AC-10", Status: core.NotMet}, {ID: "AC-3", Status: core.CannotVerify},
		},
		OutOfScope: []string{"Changes the retry policy of the email worker."},
		Summary:    "Two of three met.",
	}
}

// Issue #101 AC-1: the file has the fields of ADR 0013 section 6, the
// criteria in the order given, and the summary as the body.
func TestEncodeCheckWritesTheFileOfADR0013(t *testing.T) {
	if got := string(core.EncodeCheck(sampleCheck())); got != checkFile {
		t.Errorf("got\n%s\nwant\n%s", got, checkFile)
	}
	c, err := core.ParseCheck(sampleCheck().ID, []byte(checkFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(core.EncodeCheck(c)) != checkFile {
		t.Errorf("round trip changed the file:\n%s", core.EncodeCheck(c))
	}
	if got := core.CheckPath(checkPlanID, c.ID); got != "plans/01JD3K8Q2W5V7X9Y0Z1A2B3C4D/checks/01JD3K8Q2W5V7X9Y0Z1A2B3C4E.md" {
		t.Errorf("CheckPath = %s", got)
	}
}

func TestParseCheckRefusesABrokenFile(t *testing.T) {
	id := sampleCheck().ID
	for name, data := range map[string]string{
		"no front matter": "just text\n",
		"no author":       strings.Replace(checkFile, "  email: 41898282+github-actions[bot]@users.noreply.github.com\n", "", 1),
		"no pr":           strings.Replace(checkFile, "pr: 42\n", "", 1),
		"bad status":      strings.Replace(checkFile, "AC-2: met", "AC-2: done", 1),
		"bad yaml":        strings.Replace(checkFile, "criteria:\n", "criteria: [\n", 1),
		"duplicate AC":    strings.Replace(checkFile, "AC-3: cannot-verify", "AC-2: not-met", 1),
		"bad key":         strings.Replace(checkFile, "AC-3:", "criterion-3:", 1),
		"no plan_blob":    strings.Replace(checkFile, "plan_blob: 3b18e512dba79e4c8300dd08aeb37f8e728b8dad\n", "", 1),
		"no head_sha":     strings.Replace(checkFile, "head_sha: 9f2c\n", "", 1),
	} {
		if _, err := core.ParseCheck(id, []byte(data)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

const twoCriteriaPlan = "## Context\nx\n## Approach\nx\n## Affected areas\nx\n## Out of scope\nx\n## Tasks\n\n## Acceptance criteria\n\n- AC-1: one\n- AC-3: three\n"

// Issue #101 AC-2: a result must name every AC of plan.md, no other, each
// met, not-met or cannot-verify; the error lists every problem.
func TestCheckCriteriaAgainstThePlan(t *testing.T) {
	got, err := core.CheckCriteria([]byte(twoCriteriaPlan), map[string]string{"AC-3": "met", "AC-1": "not-met"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (core.CriterionResult{ID: "AC-1", Status: core.NotMet}) || got[1].ID != "AC-3" {
		t.Errorf("criteria = %+v, want the order of plan.md", got)
	}

	_, err = core.CheckCriteria([]byte(twoCriteriaPlan), map[string]string{"AC-1": "done", "AC-2": "met", "AC-9": "met"})
	if err == nil {
		t.Fatal("accepted a bad result")
	}
	for _, want := range []string{
		`AC-1: status "done" is not met, not-met or cannot-verify`,
		"AC-2 is not an acceptance criterion of plan.md",
		"AC-9 is not an acceptance criterion of plan.md",
		"AC-3 of plan.md has no result",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

func check(id string, pr int, blob string, criteria map[string]core.CriterionStatus, outOfScope ...string) core.Check {
	c := core.Check{ID: ulid.MustParseStrict(id), Plan: checkPlanID, PR: pr, PlanBlob: blob, OutOfScope: outOfScope}
	for _, ac := range []string{"AC-1", "AC-2"} {
		if s, ok := criteria[ac]; ok {
			c.Criteria = append(c.Criteria, core.CriterionResult{ID: ac, Status: s})
		}
	}
	return c
}

// Issue #101 AC-4: per pull request only the newest check counts; not-met
// and out-of-scope items are Findings, cannot-verify is not; a check of
// another plan.md is stale and still counts.
func TestPullRequestChecksDeriveTheFindings(t *testing.T) {
	checks := []core.Check{
		check("01JD3K8Q2W5V7X9Y0Z1A2B3C40", 7, "old", map[string]core.CriterionStatus{"AC-1": core.NotMet, "AC-2": core.NotMet}),
		check("01JD3K8Q2W5V7X9Y0Z1A2B3C41", 9, "cur", map[string]core.CriterionStatus{"AC-1": core.Met, "AC-2": core.CannotVerify}),
		check("01JD3K8Q2W5V7X9Y0Z1A2B3C42", 7, "old", map[string]core.CriterionStatus{"AC-1": core.Met, "AC-2": core.NotMet}, "Touches billing."),
		check("01JD3K8Q2W5V7X9Y0Z1A2B3C43", 12, "cur", map[string]core.CriterionStatus{"AC-1": core.NotMet}),
	}
	got := core.PullRequestChecks(checks, "cur")
	if len(got) != 3 {
		t.Fatalf("got %d pull requests, want 3: %+v", len(got), got)
	}
	pr7, pr9, pr12 := got[0], got[1], got[2]
	if pr7.Check.PR != 7 || pr9.Check.PR != 9 || pr12.Check.PR != 12 {
		t.Fatalf("order = %d, %d, %d; want by pull request", pr7.Check.PR, pr9.Check.PR, pr12.Check.PR)
	}
	if pr7.Check.ID.String() != "01JD3K8Q2W5V7X9Y0Z1A2B3C42" || !pr7.Stale {
		t.Errorf("PR 7: newest %s, stale %v", pr7.Check.ID, pr7.Stale)
	}
	want := []core.Finding{
		{PR: 7, Check: pr7.Check.ID, Criterion: "AC-2"},
		{PR: 7, Check: pr7.Check.ID, OutOfScope: "Touches billing."},
	}
	if len(pr7.Findings) != 2 || pr7.Findings[0] != want[0] || pr7.Findings[1] != want[1] {
		t.Errorf("PR 7 findings = %+v, want %+v", pr7.Findings, want)
	}
	if pr9.Stale || len(pr9.Findings) != 0 || strings.Join(pr9.Unverified, ",") != "AC-2" {
		t.Errorf("PR 9: cannot-verify is not a Finding: %+v", pr9)
	}
	if len(pr12.Findings) != 1 || pr12.Findings[0].Criterion != "AC-1" {
		t.Errorf("PR 12 findings = %+v", pr12.Findings)
	}
	if all := core.Findings(got); len(all) != 3 {
		t.Errorf("Findings = %+v, want 3", all)
	}
	if got := core.PullRequestChecks(nil, "cur"); len(got) != 0 {
		t.Errorf("no checks: %+v", got)
	}
}
