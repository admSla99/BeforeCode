package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// bfc-121s AC-1: bfc ask --option and --recommend store the options in
// order, commas kept, and the recommended one.
func TestAskWritesOptions(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, id := newPlan(t, dir, "Payments")

	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com",
		"--option", "A", "--option", "B, with comma", "--recommend", "A", "-m", "Q?"))
	data := gitIn(t, dir, "show", plansRef+":"+core.MessagePath(id, ulid.MustParseStrict(qid)))
	if !strings.Contains(data, "options: [A, 'B, with comma']\nrecommended: A\n") {
		t.Errorf("front matter:\n%s", data)
	}
	q := message(t, dir, id, qid)
	if strings.Join(q.Options, "|") != "A|B, with comma" || q.Recommended != "A" {
		t.Errorf("options %q, recommended %q", q.Options, q.Recommended)
	}
}

// bfc-121s AC-2: invalid options are refused and nothing is written.
func TestAskRefusesInvalidOptions(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	before := gitIn(t, dir, "rev-parse", plansRef)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--option", "A", "--option", "B"}, "--to"},
		{[]string{"--to", "ai", "--option", "A"}, "2 to 5"},
		{[]string{"--to", "ai", "--option", "1", "--option", "2", "--option", "3", "--option", "4", "--option", "5", "--option", "6"}, "2 to 5"},
		{[]string{"--to", "ai", "--option", "A", "--option", " "}, "empty"},
		{[]string{"--to", "ai", "--option", "A", "--option", "B\nC"}, "one line"},
		{[]string{"--to", "ai", "--option", "A", "--option", "A"}, "twice"},
		{[]string{"--to", "ai", "--recommend", "A"}, "without options"},
		{[]string{"--to", "ai", "--option", "A", "--option", "B", "--recommend", "C"}, "not one of the options"},
	} {
		args := append([]string{"ask", short, "-m", "Q?"}, c.args...)
		if out, err := bfcIn(t, dir, args...); err == nil || !strings.Contains(out, c.want) {
			t.Errorf("bfc %v: %v\n%s", args, err, out)
		}
	}
	if gitIn(t, dir, "rev-parse", plansRef) != before {
		t.Error("a refused Question changed the plans ref")
	}
}

// optionFields are the options fields of a Message in --json.
type optionFields struct {
	ID          string          `json:"id"`
	Options     json.RawMessage `json:"options"`
	Recommended json.RawMessage `json:"recommended"`
}

// bfc-121s AC-3: every Message in --json has options and recommended: the
// list and the text when set, [] and null otherwise.
func TestMessageJSONHasOptions(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")

	var asked optionFields
	out := mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--option", "A", "--option", "B", "--recommend", "B", "-m", "Q?", "--json")
	if err := json.Unmarshal([]byte(out), &asked); err != nil {
		t.Fatal(err)
	}
	const with, without = `["A","B"] "B"`, `[] null`
	compact := func(f optionFields) string {
		var o, r bytes.Buffer
		_ = json.Compact(&o, f.Options)
		_ = json.Compact(&r, f.Recommended)
		return o.String() + " " + r.String()
	}
	if got := compact(asked); got != with {
		t.Errorf("bfc ask --json: %s, want %s", got, with)
	}
	plain := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ai", "-m", "Plain?"))

	var shown struct {
		Open     []optionFields `json:"open_questions"`
		Messages []optionFields `json:"messages"`
	}
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "show", short, "--json")), &shown); err != nil {
		t.Fatal(err)
	}
	for _, list := range [][]optionFields{shown.Open, shown.Messages} {
		if len(list) != 2 || compact(list[0]) != with || list[1].ID != plain || compact(list[1]) != without {
			t.Errorf("bfc show --json: %v", list)
		}
	}

	answer := strings.TrimSpace(mustBfc(t, dir, "reply", asked.ID, "-m", "B"))
	var waited optionFields
	if err := json.Unmarshal([]byte(mustBfc(t, dir, "wait", asked.ID, "--json")), &waited); err != nil {
		t.Fatal(err)
	}
	if waited.ID != answer || compact(waited) != without {
		t.Errorf("bfc wait --json: %s %s", waited.ID, compact(waited))
	}
}

// bfc-121s T-2, AC-9: bfc show lists the options under the Question, the
// recommended one marked; an answer picked from them reads like any other.
func TestShowListsOptions(t *testing.T) {
	t.Parallel()
	dir := newLocalRepo(t)
	short, _ := newPlan(t, dir, "Payments")
	qid := strings.TrimSpace(mustBfc(t, dir, "ask", short, "--to", "ada@example.com", "--option", "A", "--option", "B", "--recommend", "B", "-m", "Q?"))
	rid := strings.TrimSpace(mustBfc(t, dir, "reply", qid, "-m", "B"))

	out := mustBfc(t, dir, "show", short)
	if !strings.Contains(out, "  Q?\n  - A\n  - B (recommended)\n") {
		t.Errorf("options missing:\n%s", out)
	}
	if !strings.Contains(out, rid) || !strings.Contains(out, "\n    B\n") {
		t.Errorf("the answer is not shown as plain text:\n%s", out)
	}
}
