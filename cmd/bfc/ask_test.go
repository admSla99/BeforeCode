package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// bfcStdin runs bfc like bfcIn, with stdin.
func bfcStdin(t *testing.T, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newRootCmd("dev")
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(inPlace(t, dir))
	return out.String(), err
}

// message reads the Message with this ULID of a plan from the plans ref.
func message(t *testing.T, dir string, plan ulid.ULID, id string) core.Message {
	t.Helper()
	data := gitIn(t, dir, "show", plansRef+":"+core.PlanDir(plan)+"/messages/"+id+".md")
	m, err := core.ParseMessage(ulid.MustParseStrict(id), []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// AC-1: bfc ask writes a Question with to and ref and prints its ULID.
func TestAskWritesAQuestion(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	before := gitIn(t, dir, "rev-parse", plansRef)

	out := mustBfc(t, dir, "ask", short, "--to", "ben@example.com", "--to", "ai", "--ref", "plan.md#AC-2", "-m", "Does it retry?")
	qid := strings.TrimSpace(out)
	q := message(t, dir, id, qid)
	if q.Author != adaP || strings.Join(q.To, ",") != "ben@example.com,ai" || q.Ref != "plan.md#AC-2" ||
		q.Body != "Does it retry?" || q.Agent != "" || q.ReplyTo != "" {
		t.Errorf("Question: %+v", q)
	}
	// One commit that adds exactly this file.
	if got := gitIn(t, dir, "rev-parse", plansRef+"~1"); got != before {
		t.Errorf("bfc ask wrote more than one commit")
	}
	changed := gitIn(t, dir, "diff", "--name-status", before, plansRef)
	if want := "A\t" + core.PlanDir(id) + "/messages/" + qid + ".md"; changed != want {
		t.Errorf("changed files:\n%s\nwant:\n%s", changed, want)
	}
}

// AC-1: the text can come on stdin, and --json prints the Message.
func TestAskReadsStdinAndPrintsJSON(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")

	out, err := bfcStdin(t, dir, "Which currency?\r\nEUR only?\n", "ask", short, "--to", "ai", "--json")
	if err != nil {
		t.Fatalf("bfc ask: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	q := message(t, dir, id, got["id"].(string))
	if q.Body != "Which currency?\nEUR only?" {
		t.Errorf("body %q", q.Body)
	}
	want := map[string]any{
		"id": q.ID.String(), "plan": id.String(), "short_id": short,
		"author": map[string]any{"name": "Ada Lovelace", "email": "ada@example.com"},
		"at":     q.At.UTC().Format("2006-01-02T15:04:05Z"),
		"to":     []any{"ai"}, "body": "Which currency?\nEUR only?",
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("JSON:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
}

// AC-2: bfc reply writes a Message with reply_to in the plan of the Message
// it replies to, found by its ULID alone.
func TestReplyAnswersInTheSamePlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	_, other := newPlan(t, dir, "Other plan")
	short, id := newPlan(t, dir, "Payments")
	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Which currency?"))

	rid := strings.TrimSpace(mustBfc(t, dir, "reply", strings.ToLower(qid), "-m", "EUR only."))
	r := message(t, dir, id, rid)
	if r.ReplyTo != qid || r.Author != adaP || r.Body != "EUR only." || r.Agent != "" || len(r.To) != 0 {
		t.Errorf("reply: %+v", r)
	}
	if out := gitIn(t, dir, "ls-tree", "--name-only", plansRef+":"+core.PlanDir(other)); strings.Contains(out, "messages") {
		t.Errorf("the other plan got a Message: %s", out)
	}
}

// AC-2: a ULID that names no Message in any plan is refused.
func TestReplyRefusesAnUnknownMessage(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	_, id := newPlan(t, dir, "Payments")
	before := gitIn(t, dir, "rev-parse", plansRef)
	for _, arg := range []string{id.String(), ulid.Make().String(), "not-a-ulid"} {
		out, err := bfcIn(t, dir, "reply", arg, "-m", "hello")
		if err == nil || !strings.Contains(out, arg) {
			t.Errorf("bfc reply %s: %v\n%s", arg, err, out)
		}
	}
	if gitIn(t, dir, "rev-parse", plansRef) != before {
		t.Error("a refused reply changed the plans ref")
	}
}

// AC-4: a malformed ref is refused; a stale one is kept.
func TestAskChecksTheFormOfRef(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	before := gitIn(t, dir, "rev-parse", plansRef)
	for _, bad := range []string{"plan.md#Affected Areas", "readme.md", "plan.md#AC-0"} {
		if out, err := bfcIn(t, dir, "ask", short, "--to", "ai", "--ref", bad, "-m", "?"); err == nil || !strings.Contains(out, "plan.md#AC-<n>") {
			t.Errorf("--ref %q: %v\n%s", bad, err, out)
		}
	}
	if gitIn(t, dir, "rev-parse", plansRef) != before {
		t.Error("a refused Question changed the plans ref")
	}
	// The plan has no plan.md, so neither ref exists: both are stale.
	for _, stale := range []string{"plan.md#AC-9", "plan.md#rollout"} {
		qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ai", "--ref", stale, "-m", "?"))
		if got := message(t, dir, id, qid).Ref; got != stale {
			t.Errorf("ref %q stored as %q", stale, got)
		}
	}
}

// Closed is final: no new Messages.
func TestAskAndReplyRefuseAClosedPlan(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ai", "-m", "?"))
	mustBfc(t, dir, "close", short)
	for _, args := range [][]string{{"ask", short, "--to", "ai", "-m", "?"}, {"reply", qid, "-m", "!"}} {
		if out, err := bfcIn(t, dir, args...); err == nil || !strings.Contains(out, "closed") {
			t.Errorf("bfc %v on a closed plan: %v\n%s", args, err, out)
		}
	}
}

// The text is required, and so is --to.
func TestAskNeedsTextAndAddressees(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	for _, c := range []struct {
		stdin string
		args  []string
		want  string
	}{
		{"", []string{"ask", short, "--to", "ai"}, "text is empty"},
		{"", []string{"ask", short, "--to", "ai", "-m", "  "}, "text is empty"},
		{"", []string{"ask", short, "-m", "?"}, "--to"},
		{"", []string{"ask", short, "--to", "ben", "-m", "?"}, `"ben"`},
	} {
		if out, err := bfcStdin(t, dir, c.stdin, c.args...); err == nil || !strings.Contains(out, c.want) {
			t.Errorf("bfc %v: %v\n%s", c.args, err, out)
		}
	}
}

// AC-3: inside Claude Code a Message is the agent's; --verbatim records the
// person's own words, which answer a Question addressed to that person.
func TestInsideAnAgentMessagesCarryTheAgent(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")
	setEnv(t, "CLAUDECODE", "1")

	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "-m", "Which currency?"))
	if q := message(t, dir, id, qid); q.Agent != "claude-code" || q.Author != adaP {
		t.Errorf("the Grill's Question: %+v", q)
	}
	rid := strings.TrimSpace(mustBfc(t, dir, "reply", qid, "-m", "I think EUR."))
	if r := message(t, dir, id, rid); r.Agent != "claude-code" {
		t.Errorf("the agent's own reply: %+v", r)
	}
	if !strings.Contains(mustBfc(t, dir, "show", short), qid) {
		t.Error("the agent's own reply answered a Question to ada")
	}
	vid := strings.TrimSpace(mustBfc(t, dir, "reply", "--verbatim", qid, "-m", "EUR only."))
	if v := message(t, dir, id, vid); v.Agent != "" || v.Author != adaP {
		t.Errorf("verbatim answer: %+v", v)
	}
	if strings.Contains(mustBfc(t, dir, "show", short), qid) {
		t.Error("the verbatim answer did not answer the Question")
	}
}

// AC-5: bfc show lists the open Questions, and bfc approve refuses while one
// is open.
func TestOpenQuestionsBlockApproval(t *testing.T) {
	t.Parallel()
	dir, short, _ := draftWithPlan(t)
	mustBfc(t, dir, "submit", short)
	as(t, dir, benP)
	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--ref", "plan.md#AC-1", "-m", "Is AC-1 enough?\nI doubt it."))
	mustBfc(t, dir, "ask", short, "--to", "ben@example.com", "-m", "A note to myself")

	out := mustBfc(t, dir, "show", short)
	want := "Open questions:\n  " + qid + "  to ada@example.com  plan.md#AC-1  Is AC-1 enough?\n"
	if !strings.Contains(out, want) || strings.Count(out, "A note to myself") != 1 {
		t.Fatalf("bfc show lacks the open Questions:\n%s\nwant:\n%s", out, want)
	}
	out, err := bfcIn(t, dir, "approve", short)
	if err == nil || !strings.Contains(out, "2 open Questions") || !strings.Contains(out, qid) {
		t.Fatalf("bfc approve with an open Question: %v\n%s", err, out)
	}
	if got := state(t, dir, short); got != "in-review" {
		t.Errorf("state %s", got)
	}

	as(t, dir, adaP)
	mustBfc(t, dir, "reply", qid, "-m", "Yes.")
	if out := mustBfc(t, dir, "show", short); strings.Contains(out, qid) {
		t.Errorf("answered Question still listed:\n%s", out)
	}
	as(t, dir, benP)
	if out, err := bfcIn(t, dir, "approve", short); err == nil || !strings.Contains(out, "1 open Question") || !strings.Contains(out, "A note to myself") {
		t.Errorf("a Question to ben stays open until ben answers: %v\n%s", err, out)
	}
}

// AC-6: two clones answer the same Question at the same time; each answer
// is its own new file, so the pushes merge without a Conflict.
func TestTwoClonesAnswerAtTheSameTime(t *testing.T) {
	t.Parallel()
	ana, ben, _ := twoClones(t)
	short, id := newPlan(t, ana, "Payments")
	qid := strings.TrimSpace(mustBfc(t, ana, "ask", short, "--to", "ben@example.com", "--to", "ai", "-m", "EUR or USD?"))
	mustBfc(t, ana, "push")
	mustBfc(t, ben, "pull")

	setEnv(t, "CLAUDECODE", "1") // ana's agent answers for the AI
	aid := strings.TrimSpace(mustBfc(t, ana, "reply", qid, "-m", "EUR, says the AI."))
	setEnv(t, "CLAUDECODE", "")
	bid := strings.TrimSpace(mustBfc(t, ben, "reply", qid, "-m", "EUR, says Ben."))
	mustBfc(t, ana, "push")
	if out := mustBfc(t, ben, "push"); !strings.Contains(out, "after merging") {
		t.Fatalf("ben's push did not merge:\n%s", out)
	}
	mustBfc(t, ana, "pull")

	for _, dir := range []string{ana, ben} {
		a, b := message(t, dir, id, aid), message(t, dir, id, bid)
		if a.ReplyTo != qid || a.Agent != "claude-code" || b.ReplyTo != qid || b.Author != benP || b.Agent != "" {
			t.Errorf("answers in %s: %+v, %+v", dir, a, b)
		}
		if out := mustBfc(t, dir, "show", short); strings.Contains(out, "Open questions") {
			t.Errorf("Question still open in %s:\n%s", dir, out)
		}
	}
	if q := message(t, ana, id, qid); q.Author != adaP {
		t.Errorf("Question: %+v", q)
	}
}
