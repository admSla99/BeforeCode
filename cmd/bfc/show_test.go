package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// showOutput is the shape of bfc show --json, documented in docs/design.md.
type showOutput struct {
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

func showAsJSON(t *testing.T, dir string, args ...string) showOutput {
	t.Helper()
	out := mustBfc(t, dir, append([]string{"show", "--json"}, args...)...)
	var got showOutput
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
	for _, empty := range []string{`"approvers": []`, `"acceptance_criteria": []`, `"open_questions": []`, `"messages": []`, `"plan": null`} {
		if !strings.Contains(out, empty) {
			t.Errorf("missing %s in\n%s", empty, out)
		}
	}
}

// ulidPattern matches a ULID in Crockford's base32.
var ulidPattern = regexp.MustCompile(`[0-9A-HJKMNP-TV-Z]{26}`)

// stableShow replaces what changes from run to run in bfc show: the ULIDs,
// numbered by first appearance, the Short ID and the times.
func stableShow(out, short string) string {
	n := map[string]string{}
	out = ulidPattern.ReplaceAllStringFunc(out, func(id string) string {
		if _, ok := n[id]; !ok {
			n[id] = fmt.Sprintf("<ULID-%d>", len(n)+1)
		}
		return n[id]
	})
	out = strings.ReplaceAll(out, short, "bfc-xxxx")
	return regexp.MustCompile(`\d{4}-\d\d-\d\d \d\d:\d\d`).ReplaceAllString(out, "<time>")
}

// onlyMessage returns the ULID of the one Message of the plan.
func onlyMessage(t *testing.T, dir string, id ulid.ULID) string {
	t.Helper()
	names := strings.Fields(gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(id)+"/messages"))
	if len(names) != 1 {
		t.Fatalf("messages = %v, want one", names)
	}
	return strings.TrimSuffix(names[0], ".md")
}

// discussedPlan is a plan by ada with a Thread and a reply, a Grill
// Question answered verbatim, and an open Question with a ref.
func discussedPlan(t *testing.T) (dir, short string) {
	t.Helper()
	dir, short, id := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, benP)
	mustBfc(t, dir, "request-changes", short, "-m", "Split the rollout into two steps.")
	thread := onlyMessage(t, dir, id)
	as(t, dir, adaP)
	mustBfc(t, dir, "reply", thread, "-m", "Done in plan.md:\ntwo steps now.")
	setEnv(t, agentEnv, "1")
	grill := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", adaP.Email, "--ref", "intent.md", "-m", "Which cards fail?\nAll of them, or some?"))
	mustBfc(t, dir, "reply", grill, "--verbatim", "-m", "Only 3-D Secure cards.")
	setEnv(t, agentEnv, "")
	mustBfc(t, dir, "ask", short, "--to", benP.Email, "--ref", "plan.md#AC-1", "-m", "Is one retry enough?")
	return dir, short
}

// Issue #88 AC-1 and AC-5: after the documents, bfc show prints every
// Thread with its replies, and marks each Question open or answered.
func TestShowPrintsTheDiscussion(t *testing.T) {
	t.Parallel()
	dir, short := discussedPlan(t)
	golden(t, stableShow(mustBfc(t, dir, "show", short), short))
}

// Issue #88 AC-2: a plan without Messages has no Discussion section.
func TestShowWithoutMessagesHasNoDiscussion(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	if out := mustBfc(t, dir, "show", short); strings.Contains(out, "Discussion") {
		t.Errorf("Discussion in\n%s", out)
	}
}

// Issue #88 AC-3: --json adds every Message, oldest first, in the shape of
// open_questions with reply_to and, for a Question, answered.
func TestShowJSONListsEveryMessage(t *testing.T) {
	t.Parallel()
	dir, short := discussedPlan(t)
	out := mustBfc(t, dir, "show", "--json", short)
	var got struct {
		OpenQuestions []json.RawMessage `json:"open_questions"`
		Messages      []map[string]any  `json:"messages"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got.OpenQuestions) != 1 {
		t.Errorf("open_questions = %d, want 1", len(got.OpenQuestions))
	}
	type entry struct {
		Body, Author, Agent, Ref string
		ReplyTo                  int // index of the replied Message, or -1
		Answered                 any // nil when not a Question
	}
	want := []entry{
		{"Split the rollout into two steps.", benP.Email, "", "", -1, nil},
		{"Done in plan.md:\ntwo steps now.", adaP.Email, "", "", 0, nil},
		{"Which cards fail?\nAll of them, or some?", adaP.Email, "claude-code", "intent.md", -1, true},
		{"Only 3-D Secure cards.", adaP.Email, "", "", 2, nil},
		{"Is one retry enough?", adaP.Email, "", "plan.md#AC-1", -1, false},
	}
	if len(got.Messages) != len(want) {
		t.Fatalf("messages = %d, want %d:\n%s", len(got.Messages), len(want), out)
	}
	for i, m := range got.Messages {
		replyTo := -1
		for j, p := range got.Messages {
			if p["id"] == m["reply_to"] {
				replyTo = j
			}
		}
		str := func(k string) string { s, _ := m[k].(string); return s }
		e := entry{str("body"), m["author"].(map[string]any)["email"].(string), str("agent"), str("ref"), replyTo, m["answered"]}
		if e != want[i] {
			t.Errorf("messages[%d] = %+v, want %+v", i, e, want[i])
		}
		if m["plan"] == nil || m["short_id"] != short || m["at"] == nil || m["id"] == nil {
			t.Errorf("messages[%d] lacks the fields of open_questions: %v", i, m)
		}
	}
	if q := got.Messages[4]; fmt.Sprint(q["to"]) != "["+benP.Email+"]" {
		t.Errorf("to = %v", q["to"])
	}
}
