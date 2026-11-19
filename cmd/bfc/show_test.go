package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/admSla99/BeforeCode/internal/core"
)

// shownJSON is the shape of bfc show --json, documented in docs/design.md.
type shownJSON struct {
	ID                 string      `json:"id"`
	ShortID            string      `json:"short_id"`
	State              string      `json:"state"`
	Title              string      `json:"title"`
	Labels             []string    `json:"labels"`
	Author             core.Person `json:"author"`
	LastActivity       string      `json:"last_activity"`
	Approvals          int         `json:"approvals"`
	RequiredApprovals  int         `json:"required_approvals"`
	Approvers          []string    `json:"approvers"`
	Intent             string      `json:"intent"`
	Plan               *string     `json:"plan"`
	AcceptanceCriteria []string    `json:"acceptance_criteria"`
	OpenQuestions      []struct {
		ID   string   `json:"id"`
		To   []string `json:"to"`
		Ref  string   `json:"ref"`
		Body string   `json:"body"`
	} `json:"open_questions"`
}

func showAsJSON(t *testing.T, dir string, args ...string) shownJSON {
	t.Helper()
	out := mustBfc(t, dir, append([]string{"show", "--json"}, args...)...)
	var got shownJSON
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return got
}

// AC-1: one JSON object with the IDs, state, author, Approvals, both
// documents, the acceptance criteria IDs and the open Questions.
func TestShowJSONPrintsThePlanForAgents(t *testing.T) {
	t.Parallel()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, benP)
	mustBfc(t, dir, "approve", short)
	q := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--ref", "plan.md#AC-1", "-m", "Which card?\nAny?"))
	mustBfc(t, dir, "ask", short, "--to", "ai", "-m", "answered")
	answered := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ai", "-m", "answered"))
	setEnv(t, agentEnv, "1")
	mustBfc(t, dir, "reply", answered, "-m", "yes")

	got := showAsJSON(t, dir, short)
	if got.ID != id.String() || got.ShortID != short || got.State != "approved" || got.Title != "Payments" ||
		got.Author.Email != "ada@example.com" || got.Approvals != 1 || got.RequiredApprovals != 1 ||
		strings.Join(got.Approvers, ",") != "ben@example.com" || got.Labels == nil {
		t.Errorf("header = %+v", got)
	}
	if !strings.HasPrefix(got.Intent, "---\n") || !strings.Contains(got.Intent, "title: Payments\n") {
		t.Errorf("intent = %q", got.Intent)
	}
	if got.Plan == nil || *got.Plan != validPlan {
		t.Errorf("plan = %v", got.Plan)
	}
	if strings.Join(got.AcceptanceCriteria, ",") != "AC-1" {
		t.Errorf("acceptance_criteria = %v", got.AcceptanceCriteria)
	}
	// The second Question to ai is open too; the third was answered.
	if len(got.OpenQuestions) != 2 || got.OpenQuestions[0].ID != q || got.OpenQuestions[0].Ref != "plan.md#AC-1" ||
		got.OpenQuestions[0].Body != "Which card?\nAny?" || strings.Join(got.OpenQuestions[0].To, ",") != "ada@example.com" {
		t.Errorf("open_questions = %+v", got.OpenQuestions)
	}
}

// AC-1: without plan.md, plan is null and the lists are empty, not null;
// without an ID it is the Current plan.
func TestShowJSONWithoutPlanMdOfTheCurrentPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	mustBfc(t, dir, "use", short)

	got := showAsJSON(t, dir)
	if got.ShortID != short || got.State != "draft" || got.Plan != nil || got.RequiredApprovals != 0 {
		t.Errorf("got %+v", got)
	}
	out := mustBfc(t, dir, "show", "--json")
	for _, empty := range []string{`"approvers": []`, `"acceptance_criteria": []`, `"open_questions": []`, `"plan": null`} {
		if !strings.Contains(out, empty) {
			t.Errorf("missing %s in\n%s", empty, out)
		}
	}
}
