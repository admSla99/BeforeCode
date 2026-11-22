package core

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.yaml.in/yaml/v3"
)

// Message is one post in the discussion of a Plan, stored as
// plans/<plan ULID>/messages/<message ULID>.md (ADR 0002 section 7).
type Message struct {
	ID     ulid.ULID // from the file name
	Author Person
	At     time.Time
	Agent  string // set when an AI wrote the text
	// To lists email addresses and/or "ai"; a Message with To is a
	// Question.
	To      []string
	ReplyTo string // ULID of the Message this one replies to
	Ref     string // the part of the plan it is about, see CheckRef
	Body    string
}

// IsQuestion reports whether m is addressed to someone.
func (m Message) IsQuestion() bool { return len(m.To) > 0 }

// Acknowledges returns the Plan check m acknowledges (ADR 0013 section 7):
// m is about the check (ref checks/<ULID>), written by a person (no Agent),
// and neither a Question nor a reply, as bfc acknowledge writes it. A
// Question or a reply about a check is discussion.
func (m Message) Acknowledges() (ulid.ULID, bool) {
	id, ok := CheckOfRef(m.Ref)
	return id, ok && m.Agent == "" && !m.IsQuestion() && m.ReplyTo == ""
}

type messageFile struct {
	Author  Person    `yaml:"author"`
	At      time.Time `yaml:"at"`
	Agent   string    `yaml:"agent,omitempty"`
	To      []string  `yaml:"to,flow,omitempty"`
	ReplyTo string    `yaml:"reply_to,omitempty"`
	Ref     string    `yaml:"ref,omitempty"`
}

// EncodeMessage returns the file content of m, with LF line endings. The ID
// is the file name and is not part of the content.
func EncodeMessage(m Message) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	err := enc.Encode(messageFile{
		Author: m.Author, At: m.At.UTC().Truncate(time.Second),
		Agent: m.Agent, To: m.To, ReplyTo: m.ReplyTo, Ref: m.Ref,
	})
	if err != nil {
		panic(err) // plain strings always encode
	}
	body := strings.TrimSpace(strings.ReplaceAll(m.Body, "\r\n", "\n"))
	b.WriteString("---\n\n" + body + "\n")
	return b.Bytes()
}

// ParseMessage reads the Message with the given ID from its file content.
// It fails when the front matter is missing or broken, or has no author
// email or time.
func ParseMessage(id ulid.ULID, data []byte) (Message, error) {
	fm, body, ok := splitFrontMatter(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	if !ok {
		return Message{}, fmt.Errorf("message %s: front matter between two --- lines is missing", id)
	}
	var f messageFile
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return Message{}, fmt.Errorf("message %s: %w", id, err)
	}
	if f.Author.Email == "" || f.At.IsZero() {
		return Message{}, fmt.Errorf("message %s: %w", id, errors.New("author email and at are required"))
	}
	return Message{
		ID: id, Author: f.Author, At: f.At, Agent: f.Agent, To: f.To, ReplyTo: f.ReplyTo, Ref: f.Ref,
		Body: strings.TrimSpace(string(body)),
	}, nil
}

// refAnchor is the part after "plan.md#": AC-<n>, or a GitHub-style heading
// slug (lowercase letters, digits, "-" and "_").
var refAnchor = regexp.MustCompile(`^(?:AC-[1-9][0-9]*|[\p{Ll}\p{Lo}\p{N}_-]+)$`)

// CheckRef reports why ref is not one of the forms of ADR 0002 section 7
// and ADR 0013 section 7: intent.md, plan.md, plan.md#<heading anchor>,
// plan.md#AC-<n> or checks/<check ULID>. It does not check that the
// heading, criterion or check exists: a stale ref is kept.
func CheckRef(ref string) error {
	switch ref {
	case "intent.md", "plan.md":
		return nil
	}
	if anchor, ok := strings.CutPrefix(ref, "plan.md#"); ok && refAnchor.MatchString(anchor) {
		return nil
	}
	if _, ok := CheckOfRef(ref); ok {
		return nil
	}
	return fmt.Errorf("ref %q is not intent.md, plan.md, plan.md#<heading anchor> (such as plan.md#affected-areas), plan.md#AC-<n> or checks/<check ULID>", ref)
}

// checksRef is the prefix of a ref to a Plan check.
const checksRef = "checks/"

// CheckRefOf is the ref of a Message about the check with this ULID.
func CheckRefOf(check ulid.ULID) string { return checksRef + check.String() }

// CheckOfRef returns the check a ref of the form checks/<ULID> names.
func CheckOfRef(ref string) (ulid.ULID, bool) {
	s, ok := strings.CutPrefix(ref, checksRef)
	if !ok {
		return ulid.ULID{}, false
	}
	id, err := ulid.ParseStrict(s)
	return id, err == nil && id.String() == s
}

// AI is the word in To that addresses a Question to the AI.
const AI = "ai"

// OpenQuestions returns the Questions among msgs that are not answered, in
// the order of msgs. A Question is answered by a direct reply whose author
// email is in To and that has no Agent, or, for AI, by a direct reply with
// an Agent (ADR 0002 section 7). Other replies are discussion.
func OpenQuestions(msgs []Message) []Message {
	replies := map[string][]Message{}
	for _, m := range msgs {
		if m.ReplyTo != "" {
			replies[m.ReplyTo] = append(replies[m.ReplyTo], m)
		}
	}
	var open []Message
	for _, q := range msgs {
		if q.IsQuestion() && !answered(q, replies[q.ID.String()]) {
			open = append(open, q)
		}
	}
	return open
}

// DiscussionMessage is a Message as the Discussion lists it.
type DiscussionMessage struct {
	Message
	// Depth is 0 for the first Message of a Thread, 1 for a reply to it,
	// and so on.
	Depth int
	// Answered is set for a Question that is answered (see OpenQuestions).
	Answered bool
}

// Discussion returns every Message of msgs, given in ULID order, as the
// Threads read: each Thread in the order of its first Message, each reply
// after the Message it replies to, oldest first (ADR 0002 section 7). A
// reply to a Message that is not in msgs starts a Thread of its own, and
// replies in a cycle are listed last, so no Message is left out.
func Discussion(msgs []Message) []DiscussionMessage {
	known := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		known[m.ID.String()] = true
	}
	replies := map[string][]Message{}
	for _, m := range msgs {
		if known[m.ReplyTo] {
			replies[m.ReplyTo] = append(replies[m.ReplyTo], m)
		}
	}
	out := make([]DiscussionMessage, 0, len(msgs))
	seen := make(map[ulid.ULID]bool, len(msgs))
	var walk func(m Message, depth int)
	walk = func(m Message, depth int) {
		if seen[m.ID] {
			return
		}
		seen[m.ID] = true
		rs := replies[m.ID.String()]
		out = append(out, DiscussionMessage{Message: m, Depth: depth, Answered: m.IsQuestion() && answered(m, rs)})
		for _, r := range rs {
			walk(r, depth+1)
		}
	}
	for _, m := range msgs {
		if !known[m.ReplyTo] {
			walk(m, 0)
		}
	}
	for _, m := range msgs {
		walk(m, 0)
	}
	return out
}

func answered(q Message, replies []Message) bool {
	_, ok := firstAnswer(q, replies)
	return ok
}

// firstAnswer returns the first of replies, direct replies to q, that
// answers it.
func firstAnswer(q Message, replies []Message) (Message, bool) {
	for _, r := range replies {
		for _, to := range q.To {
			if to == AI && r.Agent != "" || r.Agent == "" && strings.EqualFold(to, r.Author.Email) {
				return r, true
			}
		}
	}
	return Message{}, false
}

// AnswerOf returns the Message among msgs, in ULID order, that answers the
// Question with this ID (the rule of OpenQuestions); ok is false while it
// is open, or when the Question is not among msgs.
func AnswerOf(msgs []Message, question ulid.ULID) (answer Message, ok bool) {
	var q *Message
	var replies []Message
	for i, m := range msgs {
		switch {
		case m.ID == question:
			q = &msgs[i]
		case m.ReplyTo == question.String():
			replies = append(replies, m)
		}
	}
	if q == nil {
		return Message{}, false
	}
	return firstAnswer(*q, replies)
}

// CheckOpenQuestions reports why the plan with Status st cannot be approved
// while the given Questions are open, or nil when there are none (ADR 0002
// section 4).
func CheckOpenQuestions(st Status, open []Message) error {
	if len(open) == 0 {
		return nil
	}
	noun := "Questions"
	if len(open) == 1 {
		noun = "Question"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d open %s; answer them with bfc reply <ULID> before approving:", st.ShortID, len(open), noun)
	for _, q := range open {
		fmt.Fprintf(&b, "\n  %s  to %s  %s", q.ID, strings.Join(q.To, ", "), firstLine(q.Body))
	}
	return errors.New(b.String())
}

// firstLine is the first line of s.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
