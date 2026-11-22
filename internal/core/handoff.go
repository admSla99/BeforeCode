package core

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.yaml.in/yaml/v3"
)

// Handoff is the note the holder of a Task leaves when they stop working on
// it, stored as plans/<plan ULID>/handoff/<Handoff ULID>.md (ADR 0013
// section 5). Its released or completed Event names it; Done says which.
type Handoff struct {
	ID     ulid.ULID // from the file name
	Author Person
	At     time.Time
	Agent  string
	Task   string
	Done   bool
	Note   string // Markdown
}

// HandoffPath is the file of the Handoff of the plan with ULID plan.
func HandoffPath(plan, handoff ulid.ULID) string {
	return PlanDir(plan) + "/handoff/" + handoff.String() + ".md"
}

type handoffFile struct {
	Author Person    `yaml:"author"`
	At     time.Time `yaml:"at"`
	Agent  string    `yaml:"agent,omitempty"`
	Task   string    `yaml:"task"`
	Done   bool      `yaml:"done"`
}

// EncodeHandoff returns the file content of h, with LF line endings.
func EncodeHandoff(h Handoff) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	err := enc.Encode(handoffFile{Author: h.Author, At: h.At.UTC().Truncate(time.Second), Agent: h.Agent, Task: h.Task, Done: h.Done})
	if err != nil {
		panic(err) // plain strings and a bool always encode
	}
	note := strings.TrimSpace(strings.ReplaceAll(h.Note, "\r\n", "\n"))
	b.WriteString("---\n\n" + note + "\n")
	return b.Bytes()
}

// ParseHandoff reads the Handoff with the given ID from its file content.
// It fails when the front matter is missing or broken, lacks the author
// email or the time, names no Task, or the note is empty.
func ParseHandoff(id ulid.ULID, data []byte) (Handoff, error) {
	fail := func(err error) (Handoff, error) { return Handoff{}, fmt.Errorf("handoff %s: %w", id, err) }
	fm, body, ok := splitFrontMatter(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	if !ok {
		return fail(errors.New("front matter between two --- lines is missing"))
	}
	var f handoffFile
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return fail(err)
	}
	note := strings.TrimSpace(string(body))
	switch {
	case f.Author.Email == "" || f.At.IsZero():
		return fail(errors.New("author email and at are required"))
	case !taskID.MatchString(f.Task):
		return fail(fmt.Errorf("task %q is not a Task ID such as T-1", f.Task))
	case note == "":
		return fail(errors.New("the note is empty"))
	}
	return Handoff{ID: id, Author: f.Author, At: f.At, Agent: f.Agent, Task: f.Task, Done: f.Done, Note: note}, nil
}
