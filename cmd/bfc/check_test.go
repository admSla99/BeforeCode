package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// A plan check result as the workflow passes it to bfc check record.
const sampleResult = `{"criteria": {"AC-1": "not-met"}, "out_of_scope": ["Touches the email worker."], "summary": "AC-1 is not met.\n\nSee the diff."}`

// checkOutput is the shape of bfc check record --json and of the checks of
// bfc show --json, documented in docs/design.md.
type checkOutput struct {
	ID       string      `json:"id"`
	Plan     string      `json:"plan"`
	ShortID  string      `json:"short_id"`
	Author   core.Person `json:"author"`
	At       string      `json:"at"`
	Agent    string      `json:"agent"`
	PR       int         `json:"pr"`
	HeadSHA  string      `json:"head_sha"`
	BaseSHA  string      `json:"base_sha"`
	PlanBlob string      `json:"plan_blob"`
	Criteria []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"criteria"`
	OutOfScope []string `json:"out_of_scope"`
	Summary    string   `json:"summary"`
	Stale      *bool    `json:"stale"`
}

type findingOutput struct {
	PR         int    `json:"pr"`
	Check      string `json:"check"`
	Criterion  string `json:"criterion"`
	OutOfScope string `json:"out_of_scope"`
}

func recordResult(t *testing.T, dir, short string, pr, result string, extra ...string) string {
	t.Helper()
	args := append([]string{"check", "record", short, "--pr", pr, "--head", "9f2c", "--base", "1a7e"}, extra...)
	return mustStdin(t, dir, result, args...)
}

// Issue #101 AC-1, AC-3: check record writes one check file in one commit
// with the fields of ADR 0013 section 6 and prints its ULID.
func TestCheckRecordWritesTheCheckFile(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := strings.TrimSpace(recordResult(t, dir, short, "42", sampleResult))

	checkID, err := ulid.ParseStrict(out)
	if err != nil {
		t.Fatalf("output is not a ULID: %q", out)
	}
	if parent := gitIn(t, dir, "rev-parse", plansRef+"^"); parent != before {
		t.Errorf("check record made more than one commit")
	}
	if diff := gitIn(t, dir, "diff", "--name-only", plansRef+"^", plansRef); diff != core.CheckPath(id, checkID) {
		t.Errorf("commit changed %q", diff)
	}
	file := gitIn(t, dir, "show", plansRef+":"+core.CheckPath(id, checkID))
	c, err := core.ParseCheck(checkID, []byte(file))
	if err != nil {
		t.Fatalf("%v\n%s", err, file)
	}
	if c.Plan != id || c.PR != 42 || c.HeadSHA != "9f2c" || c.BaseSHA != "1a7e" || c.PlanBlob != planBlob(t, dir, id) ||
		c.Author.Email != "ada@example.com" || c.At.IsZero() || c.Agent != "" ||
		len(c.Criteria) != 1 || c.Criteria[0] != (core.CriterionResult{ID: "AC-1", Status: core.NotMet}) ||
		strings.Join(c.OutOfScope, "|") != "Touches the email worker." || c.Summary != "AC-1 is not met.\n\nSee the diff." {
		t.Errorf("check = %+v\n%s", c, file)
	}
}

// Issue #101 AC-3: --json prints the stored check.
func TestCheckRecordJSON(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	setEnv(t, agentEnv, "1")

	out := recordResult(t, dir, short, "7", sampleResult, "--json")

	var got checkOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Plan != id.String() || got.ShortID != short || got.PR != 7 || got.PlanBlob != planBlob(t, dir, id) ||
		got.Agent != "claude-code" || len(got.Criteria) != 1 || got.Criteria[0].Status != "not-met" || got.Summary == "" {
		t.Errorf("got %+v", got)
	}
	if _, err := ulid.ParseStrict(got.ID); err != nil {
		t.Errorf("id %q", got.ID)
	}
}

// Issue #101 AC-2: a result that does not match plan.md is refused with
// every problem, and nothing is written.
func TestCheckRecordRefusesAResultThatDoesNotMatchThePlan(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	before := planCommits(t, dir)

	for name, c := range map[string]struct{ result, want string }{
		"bad result": {`{"criteria": {"AC-2": "met", "AC-3": "maybe"}, "summary": "x"}`,
			"AC-2 is not an acceptance criterion of plan.md\nAC-3 is not an acceptance criterion of plan.md\nAC-1 of plan.md has no result"},
		"bad status":   {`{"criteria": {"AC-1": "maybe"}, "summary": "x"}`, `AC-1: status "maybe" is not met, not-met or cannot-verify`},
		"no summary":   {`{"criteria": {"AC-1": "met"}}`, "summary"},
		"not JSON":     {`AC-1 met`, "JSON"},
		"unknown key":  {`{"criteria": {"AC-1": "met"}, "summary": "x", "verdict": "ok"}`, "verdict"},
		"empty string": {``, "JSON"},
	} {
		out, err := bfcStdin(t, dir, c.result, "check", "record", short, "--pr", "1", "--head", "h", "--base", "b")
		if err == nil || !strings.Contains(out, c.want) {
			t.Errorf("%s: err %v, output lacks %q:\n%s", name, err, c.want, out)
		}
	}
	out, err := bfcStdin(t, dir, sampleResult, "check", "record", short, "--head", "h", "--base", "b")
	if err == nil || !strings.Contains(out, "--pr") {
		t.Errorf("without --pr: err %v\n%s", err, out)
	}
	if planCommits(t, dir) != before {
		t.Errorf("a refused result was written")
	}
}

// Issue #101 AC-3: a closed plan takes no checks.
func TestCheckRecordRefusesAClosedPlan(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "close", short)

	out, err := bfcStdin(t, dir, sampleResult, "check", "record", short, "--pr", "1", "--head", "h", "--base", "b")
	if err == nil || !strings.Contains(out, "closed") {
		t.Errorf("err %v\n%s", err, out)
	}
}

// Issue #101 AC-5: bfc show --json has the newest check of each pull
// request, stale when plan.md changed since, and the Findings.
func TestShowJSONListsChecksAndFindings(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	recordResult(t, dir, short, "7", sampleResult)
	newest := strings.TrimSpace(recordResult(t, dir, short, "7", `{"criteria": {"AC-1": "cannot-verify"}, "out_of_scope": ["Adds a cron job."], "summary": "x"}`))
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan + "\nMore context.\n")})
	clean := strings.TrimSpace(recordResult(t, dir, short, "9", `{"criteria": {"AC-1": "met"}, "summary": "All met."}`))

	out := mustBfc(t, dir, "show", short, "--json")
	var got struct {
		Checks   []checkOutput   `json:"checks"`
		Findings []findingOutput `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Checks) != 2 || got.Checks[0].ID != newest || got.Checks[1].ID != clean {
		t.Fatalf("checks = %+v", got.Checks)
	}
	if got.Checks[0].Stale == nil || !*got.Checks[0].Stale || got.Checks[1].Stale == nil || *got.Checks[1].Stale {
		t.Errorf("stale markers wrong: %+v", got.Checks)
	}
	if len(got.Findings) != 1 || got.Findings[0] != (findingOutput{PR: 7, Check: newest, OutOfScope: "Adds a cron job."}) {
		t.Errorf("findings = %+v", got.Findings)
	}

	empty := mustBfc(t, dir, "show", "--json", strings.Fields(mustBfc(t, dir, "new", "--no-edit", "Other"))[0])
	for _, want := range []string{`"checks": []`, `"findings": []`} {
		if !strings.Contains(empty, want) {
			t.Errorf("missing %s in\n%s", want, empty)
		}
	}
}

// Issue #101 AC-5: bfc show prints a Checks section with the newest check
// of each pull request, its Findings and the stale marker.
func TestShowPrintsTheChecks(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	first := strings.TrimSpace(recordResult(t, dir, short, "7", sampleResult))
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan + "\nMore context.\n")})

	out := mustBfc(t, dir, "show", short)
	for _, want := range []string{
		"\n--- Checks ---\n",
		"PR #7  " + first + "  stale: plan.md changed since",
		"  not met: AC-1",
		"  out of scope: Touches the email worker.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(mustBfc(t, dir, "show", strings.Fields(mustBfc(t, dir, "new", "--no-edit", "Other"))[0]), "--- Checks ---") {
		t.Errorf("a plan without checks has a Checks section")
	}
}

// Issue #101 AC-6: bfc doctor warns about a check file it cannot read.
func TestDoctorWarnsAboutAnUnreadableCheckFile(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	recordResult(t, dir, short, "7", sampleResult)
	bad := core.CheckPath(id, ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E"))
	writeRef(t, dir, "broken check", map[string][]byte{bad: []byte("not a check\n")})

	out, _ := bfcIn(t, dir, "doctor")
	wantDoctor(t, out, "warn", "check files", "1 of 2 cannot be read")
	if !strings.Contains(out, bad) {
		t.Errorf("output does not name %s:\n%s", bad, out)
	}
}

// Issue #103 AC-2: check find names the plan of a pull request from its
// branch, else its description, as JSON for the workflow; no plan is
// found: false and exit 0.
func TestCheckFindNamesThePlanOfAPullRequest(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	body := filepath.Join(t.TempDir(), "body.md")
	writeFile(t, body, "Fixes the checkout.\n\nPlan: "+short+"\n")

	type found struct {
		Found   bool   `json:"found"`
		ID      string `json:"id"`
		ShortID string `json:"short_id"`
		Title   string `json:"title"`
		State   string `json:"state"`
	}
	find := func(args ...string) found {
		t.Helper()
		var got found
		out := mustBfc(t, dir, append([]string{"check", "find", "--json"}, args...)...)
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		return got
	}
	want := found{Found: true, ID: id.String(), ShortID: short, Title: "Payments", State: "draft"}
	if got := find("--branch", short+"-payments"); got != want {
		t.Errorf("from the branch: %+v", got)
	}
	if got := find("--branch", "fix-checkout", "--body-file", body); got != want {
		t.Errorf("from the description: %+v", got)
	}
	if got := find("--branch", "fix-checkout"); got != (found{}) {
		t.Errorf("no plan: %+v", got)
	}
	if out := strings.TrimSpace(mustBfc(t, dir, "check", "find", "--branch", short+"-x")); out != short {
		t.Errorf("without --json: %q", out)
	}
	if out, err := bfcIn(t, dir, "check", "find", "--branch", "bfc-zzzz-x"); err == nil {
		t.Errorf("a branch that names no plan: %s", out)
	}
}

// Issue #138 AC-4: the agent pipes the description of the pull request
// from gh pr view into bfc check find, and may run bfc check without asking.
func TestCheckFindReadsTheDescriptionFromStdin(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	out, err := bfcStdin(t, dir, "Fixes the checkout.\n\nPlan: "+short+"\n", "check", "find", "--branch", "fix-checkout", "--body-file", "-")
	if err != nil || strings.TrimSpace(out) != short {
		t.Errorf("from stdin: %v\n%s", err, out)
	}

	mustBfc(t, dir, "setup", "claude")
	settings := readFile(t, filepath.Join(dir, ".claude", "settings.json"))
	if !strings.Contains(settings, `"Bash(bfc check:*)"`) {
		t.Errorf("settings do not allow bfc check:\n%s", settings)
	}
}
