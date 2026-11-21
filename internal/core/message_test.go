package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// AC-4: a ref has one of the forms of ADR 0002 section 7. Whether the
// heading or criterion still exists is not checked: a stale ref is kept.
func TestCheckRef(t *testing.T) {
	for _, ref := range []string{
		"intent.md", "plan.md", "plan.md#affected-areas", "plan.md#AC-2", "plan.md#AC-17",
		"plan.md#out-of-scope", "plan.md#step_2", "plan.md#čo-nie", "plan.md#AC-99",
	} {
		if err := CheckRef(ref); err != nil {
			t.Errorf("CheckRef(%q): %v", ref, err)
		}
	}
	for _, ref := range []string{
		"", "readme.md", "intent.md#problem", "plan.md#", "plan.md#Affected-Areas", "plan.md#affected areas",
		"plan.md#AC-0", "plan.md#AC-02", "plan.md#AC-", "PLAN.md", "plan.md#a#b", "plans/x/plan.md",
	} {
		if err := CheckRef(ref); err == nil {
			t.Errorf("CheckRef(%q) accepted a malformed ref", ref)
		}
	}
}

// The Question of ADR 0002 section 7, as bfc writes it.
const questionFile = `---
author:
  name: Peter Horváth
  email: peter@example.com
at: 2026-11-16T21:42:00Z
agent: claude-code
to: [jana@example.com, ai]
reply_to: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D
ref: plan.md#AC-2
---

Does "exactly one" also apply when the customer retries the payment?
`

func TestEncodeMessageWritesEveryField(t *testing.T) {
	m := Message{
		Author:  Person{Name: "Peter Horváth", Email: "peter@example.com"},
		At:      time.Date(2026, 11, 16, 21, 42, 0, 0, time.UTC),
		Agent:   "claude-code",
		To:      []string{"jana@example.com", "ai"},
		ReplyTo: "01JD3K8Q2W5V7X9Y0Z1A2B3C4D",
		Ref:     "plan.md#AC-2",
		Body:    "Does \"exactly one\" also apply when the customer retries the payment?\r\n",
	}
	if got := string(EncodeMessage(m)); got != questionFile {
		t.Errorf("EncodeMessage:\n%s\nwant:\n%s", got, questionFile)
	}
}

func TestParseMessage(t *testing.T) {
	id := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4E")
	m, err := ParseMessage(id, []byte(questionFile))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != id || m.Author.Email != "peter@example.com" || m.Agent != "claude-code" ||
		strings.Join(m.To, ",") != "jana@example.com,ai" || m.ReplyTo != "01JD3K8Q2W5V7X9Y0Z1A2B3C4D" ||
		m.Ref != "plan.md#AC-2" || !m.At.Equal(time.Date(2026, 11, 16, 21, 42, 0, 0, time.UTC)) ||
		m.Body != `Does "exactly one" also apply when the customer retries the payment?` {
		t.Errorf("ParseMessage: %+v", m)
	}
	if !m.IsQuestion() {
		t.Error("a Message with to is a Question")
	}
	for _, bad := range []string{"", "no front matter\n", "---\nat: [\n---\n\nx\n", "---\nat: 2026-11-16T21:42:00Z\n---\n\nno author\n"} {
		if _, err := ParseMessage(id, []byte(bad)); err == nil {
			t.Errorf("ParseMessage(%q) accepted a broken file", bad)
		}
	}
}

func msg(n byte, author Person, agent string, to []string, replyTo byte) Message {
	m := Message{ID: ulid.ULID{15: n}, Author: author, Agent: agent, To: to}
	if replyTo != 0 {
		m.ReplyTo = ulid.ULID{15: replyTo}.String()
	}
	return m
}

func ids(ms []Message) []byte {
	var out []byte
	for _, m := range ms {
		out = append(out, m.ID[15])
	}
	return out
}

// AC-5 and ADR 0002 section 7: a Question is answered by a direct reply from
// a person it is addressed to, written without an agent, or for "ai" by a
// direct reply with an agent.
func TestOpenQuestions(t *testing.T) {
	msgs := []Message{
		msg(1, ben, "claude-code", []string{"ada@example.com"}, 0), // the Grill asks ada
		msg(2, ada, "", nil, 1), // ada's answer, recorded verbatim
		msg(3, ben, "claude-code", []string{"ADA@example.com"}, 0), // asked again
		msg(4, ada, "claude-code", nil, 3),                         // the agent itself, not ada
		msg(5, ben, "", nil, 3),                                    // discussion
		msg(6, ada, "", []string{"ai"}, 0),                         // a question to the AI
		msg(7, ben, "", nil, 6),                                    // a person does not answer for the AI
		msg(8, ada, "", []string{"ai", "ben@example.com"}, 0),      // either may answer
		msg(9, ada, "claude-code", nil, 8),
		msg(10, ada, "", []string{"ben@example.com"}, 0),
		msg(11, ben, "", nil, 12), // a reply to a reply does not count
		msg(12, jana, "", nil, 10),
		msg(13, ada, "", nil, 0), // not a Question
	}
	if got, want := ids(OpenQuestions(msgs)), []byte{3, 6, 10}; string(got) != string(want) {
		t.Errorf("open Questions %v, want %v", got, want)
	}
}

func TestCheckApproveRefusesWhileAQuestionIsOpen(t *testing.T) {
	open := []Message{msg(1, ben, "claude-code", []string{"ada@example.com"}, 0)}
	wantErr(t, CheckOpenQuestions(status(StateInReview), open), "bfc-abcd", "1 open Question", open[0].ID.String(), "bfc reply")
	if err := CheckOpenQuestions(status(StateInReview), nil); err != nil {
		t.Errorf("no open Questions: %v", err)
	}
}

// Issue #88: the Discussion lists every Thread in the order of its first
// Message, each reply under the Message it replies to, and marks each
// Question answered or open by the rule of ADR 0002 section 7.
func TestDiscussionOrdersThreadsAndReplies(t *testing.T) {
	msgs := []Message{
		msg(1, ben, "claude-code", []string{"ada@example.com"}, 0), // a Grill Question
		msg(2, ada, "", []string{"ben@example.com"}, 0),            // an open Question
		msg(3, ada, "", nil, 1),                                    // answers 1
		msg(4, ben, "", nil, 3),                                    // a reply to the answer
		msg(5, jana, "", nil, 1),                                   // more discussion of 1
		msg(6, jana, "", nil, 0),                                   // a Thread without a Question
		msg(7, ben, "", nil, 99),                                   // replies to a missing Message
	}
	type line struct {
		id, depth   byte
		q, answered bool
	}
	want := []line{
		{1, 0, true, true}, {3, 1, false, false}, {4, 2, false, false}, {5, 1, false, false},
		{2, 0, true, false},
		{6, 0, false, false},
		{7, 0, false, false},
	}
	var got []line
	for _, p := range Discussion(msgs) {
		got = append(got, line{p.ID[15], byte(p.Depth), p.IsQuestion(), p.Answered})
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Discussion = %v, want %v", got, want)
	}
}

// Issue #88: replies that point at each other form no Thread with a first
// Message; they are still listed, so nothing in the plans ref is hidden.
func TestDiscussionKeepsRepliesInACycle(t *testing.T) {
	msgs := []Message{msg(1, ben, "", nil, 2), msg(2, ada, "", nil, 1)}
	if got := len(Discussion(msgs)); got != 2 {
		t.Errorf("Discussion has %d Messages, want 2", got)
	}
}
