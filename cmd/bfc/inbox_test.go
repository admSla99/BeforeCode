package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

// inboxFixture: Ada's plan with an unacknowledged check on PR 7, Jana's
// plan in review that Ada may approve, and a Question from Ben to Ada.
func inboxFixture(t *testing.T) (dir, mine, theirs, check, question string) {
	t.Helper()
	dir, mine, _ = draftWithPlan(t)
	check = strings.TrimSpace(recordResult(t, dir, mine, "7", sampleResult))
	as(t, dir, benP)
	question = strings.TrimSpace(mustBfc(t, dir, "ask", mine, "--to", "ada@example.com", "-m", "Which cards fail?"))
	as(t, dir, janaP)
	theirs, id := newPlan(t, dir, "Refunds")
	writeRef(t, dir, "plan", map[string][]byte{core.PlanDir(id) + "/plan.md": []byte(validPlan)})
	mustBfc(t, dir, "submit", theirs)
	as(t, dir, adaP)
	return dir, mine, theirs, check, question
}

// Issue #106 AC-2: bfc inbox prints the three lists, and --json has them
// with the git author, as lists that are never null.
func TestInboxListsWhatWaitsOnMe(t *testing.T) {
	t.Parallel()
	dir, mine, theirs, check, question := inboxFixture(t)

	out := mustBfc(t, dir, "inbox")
	for _, want := range []string{
		"Findings (2)\n",
		"  " + mine + "  PR #7  AC-1 is not met  (Payments)\n",
		"  " + mine + "  PR #7  out of scope: Touches the email worker.  (Payments)\n",
		"Reviews (1)\n  " + theirs + "  Refunds\n",
		"Questions (1)\n  " + mine + "  " + question + "  from ben@example.com: Which cards fail?\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("bfc inbox lacks %q:\n%s", want, out)
		}
	}

	var got struct {
		Identity core.Person `json:"identity"`
		Findings []struct {
			ShortID    string `json:"short_id"`
			Title      string `json:"title"`
			PR         int    `json:"pr"`
			Check      string `json:"check"`
			Criterion  string `json:"criterion"`
			OutOfScope string `json:"out_of_scope"`
		} `json:"findings"`
		Reviews []struct {
			Plan    string `json:"plan"`
			ShortID string `json:"short_id"`
		} `json:"reviews"`
		Questions []struct {
			ID      string `json:"id"`
			ShortID string `json:"short_id"`
			Body    string `json:"body"`
		} `json:"questions"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "inbox", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Identity.Email != adaP.Email || len(got.Findings) != 2 || got.Findings[0].Check != check || got.Findings[0].Criterion != "AC-1" ||
		got.Findings[1].OutOfScope != "Touches the email worker." || got.Findings[0].PR != 7 {
		t.Errorf("findings = %+v", got.Findings)
	}
	if len(got.Reviews) != 1 || got.Reviews[0].ShortID != theirs || len(got.Reviews[0].Plan) != 26 {
		t.Errorf("reviews = %+v", got.Reviews)
	}
	if len(got.Questions) != 1 || got.Questions[0].ID != question || got.Questions[0].ShortID != mine {
		t.Errorf("questions = %+v", got.Questions)
	}
}

// Issue #106 AC-1, AC-4: an Acknowledgement or a clean newer check takes
// the Findings out; nothing waiting is one line, and [] in JSON.
func TestInboxEmptiesWhenFindingsAreSettled(t *testing.T) {
	t.Parallel()
	dir, mine, theirs, check, question := inboxFixture(t)
	mustBfc(t, dir, "acknowledge", check, "-m", "Agreed.")
	recordResult(t, dir, mine, "8", sampleResult)
	recordResult(t, dir, mine, "8", `{"criteria": {"AC-1": "met"}, "summary": "Clean."}`)
	mustBfc(t, dir, "reply", question, "-m", "Visa.")
	mustBfc(t, dir, "close", theirs)

	if out := mustBfc(t, dir, "inbox"); out != "Nothing waits on you.\n" {
		t.Errorf("bfc inbox:\n%s", out)
	}
	js := mustBfc(t, dir, "inbox", "--json")
	for _, want := range []string{`"findings": []`, `"reviews": []`, `"questions": []`} {
		if !strings.Contains(js, want) {
			t.Errorf("missing %s in\n%s", want, js)
		}
	}
}

// Issue #106 AC-1: Prime's Waiting on you has the Findings too.
func TestPrimeListsFindingsWaitingOnMe(t *testing.T) {
	t.Parallel()
	dir, mine, _, check, _ := inboxFixture(t)
	out := mustBfc(t, dir, "prime")
	want := "- Finding in " + mine + " Payments, pull request #7: AC-1 is not met (check " + check + ")."
	if !strings.Contains(out, want) || !strings.Contains(out, "bfc acknowledge <check>") {
		t.Errorf("prime lacks %q:\n%s", want, out)
	}
}
