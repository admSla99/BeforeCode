package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// The Handoff of ADR 0013 section 5, as bfc writes it.
const handoffFile = `---
author:
  name: Jana Nováková
  email: jana@example.com
at: 2026-11-22T10:15:00Z
agent: claude-code
task: T-2
done: false
---

The webhook handler is in place; the idempotency key is not stored yet.
`

// Issue #109 AC-1: a Handoff file has the common fields, task and done,
// then the note.
func TestHandoffFile(t *testing.T) {
	id := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C9H")
	h := core.Handoff{
		ID: id, Author: core.Person{Name: "Jana Nováková", Email: "jana@example.com"},
		At: time.Date(2026, 11, 22, 10, 15, 0, 0, time.UTC), Agent: "claude-code", Task: "T-2",
		Note: "The webhook handler is in place; the idempotency key is not stored yet.\n",
	}
	if got := string(core.EncodeHandoff(h)); got != handoffFile {
		t.Errorf("got\n%s\nwant\n%s", got, handoffFile)
	}
	got, err := core.ParseHandoff(id, []byte(handoffFile))
	if err != nil {
		t.Fatal(err)
	}
	if got.Task != "T-2" || got.Done || got.Author.Email != "jana@example.com" || got.Note != "The webhook handler is in place; the idempotency key is not stored yet." {
		t.Errorf("parsed %+v", got)
	}
	planID := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	if p := core.HandoffPath(planID, id); p != "plans/01JD3K8Q2W5V7X9Y0Z1A2B3C4D/handoff/01JD3K8Q2W5V7X9Y0Z1A2B3C9H.md" {
		t.Errorf("HandoffPath = %s", p)
	}
	for name, data := range map[string]string{
		"no front matter": "note only\n",
		"no task":         strings.Replace(handoffFile, "task: T-2\n", "", 1),
		"bad task":        strings.Replace(handoffFile, "task: T-2", "task: 2", 1),
		"no author":       strings.Replace(handoffFile, "  email: jana@example.com\n", "", 1),
		"empty note":      strings.SplitAfter(handoffFile, "---\n")[0] + strings.SplitAfter(handoffFile, "---\n")[1] + "\n",
	} {
		if _, err := core.ParseHandoff(id, []byte(data)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
