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

// Plan is the unit of work: a directory plans/<ULID>/ in the plans ref.
type Plan struct {
	ID      ulid.ULID
	ShortID string
	Title   string
	Author  Person // the plan author, from the created event
}

// Intent is the front matter of intent.md (ADR 0002 section 5).
type Intent struct {
	Title  string   `yaml:"title"`
	Labels []string `yaml:"labels"`
}

var labelPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// PlansDir is the directory of the plans ref that holds one directory per
// Plan.
const PlansDir = "plans"

// PlanDir returns the plan's directory in the plans ref.
func PlanDir(id ulid.ULID) string {
	return PlansDir + "/" + id.String()
}

// MessagePath is the file of the Message msg of the plan with ULID plan
// (ADR 0002 section 7).
func MessagePath(plan, msg ulid.ULID) string {
	return PlanDir(plan) + "/messages/" + msg.String() + ".md"
}

// NewPlan creates a plan from the text of its intent.md. newID hands out
// ULIDs in increasing order: the first names the plan, the second its
// created event. existing are the ULIDs of the plans already in the ref,
// which decide the length of the Short ID. NewPlan returns the plan and the
// files to write to the plans ref, keyed by path.
func NewPlan(intent []byte, author Person, at time.Time, newID func() ulid.ULID, existing []ulid.ULID) (Plan, map[string][]byte, error) {
	in, err := ParseIntent(intent)
	if err != nil {
		return Plan{}, nil, err
	}
	if author.Email == "" {
		return Plan{}, nil, errors.New("author email is missing; set user.email in git config")
	}
	id := newID()
	plan := Plan{ID: id, ShortID: ShortID(id, existing), Title: in.Title, Author: author}
	created := Event{ID: newID(), Type: Created, Plan: id, Author: author, At: at, ShortID: plan.ShortID}
	files := map[string][]byte{
		PlanDir(id) + "/intent.md":                               intent,
		PlanDir(id) + "/events/" + created.ID.String() + ".yaml": EncodeEvent(created),
	}
	return plan, files, nil
}

// ParseIntent reads and checks the front matter of intent.md: a title of one
// non-empty line and labels of lower-case words.
func ParseIntent(data []byte) (Intent, error) {
	fm, _, ok := splitFrontMatter(data)
	if !ok {
		return Intent{}, errors.New("intent.md: front matter between two --- lines is missing")
	}
	var in Intent
	if err := yaml.Unmarshal(fm, &in); err != nil {
		return Intent{}, fmt.Errorf("intent.md: front matter: %w", err)
	}
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "":
		return Intent{}, errors.New("intent.md: title is missing")
	case strings.ContainsAny(in.Title, "\r\n"):
		return Intent{}, errors.New("intent.md: title must be one line")
	}
	for _, l := range in.Labels {
		if !labelPattern.MatchString(l) {
			return Intent{}, fmt.Errorf("intent.md: label %q must use only a-z, 0-9 and -", l)
		}
	}
	return in, nil
}

// IntentBody returns the text of intent.md after its front matter, or all
// of it when there is no front matter.
func IntentBody(data []byte) []byte {
	if _, body, ok := splitFrontMatter(data); ok {
		return body
	}
	return data
}

// FillIntentTemplate returns the intent.md template with its title set.
// The rest of the template is kept as it is.
func FillIntentTemplate(template []byte, title string) []byte {
	line := "title: " + yamlScalar(title)
	fm, body, ok := splitFrontMatter(template)
	if !ok {
		return []byte("---\n" + line + "\n---\n\n" + string(template))
	}
	lines := strings.Split(string(fm), "\n")
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(l, "title:") {
			lines[i] = line
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append([]string{line}, lines...)
	}
	return []byte("---\n" + strings.Join(lines, "\n") + "---\n" + string(body))
}

// splitFrontMatter splits a Markdown file into its YAML front matter
// (including the final newline) and the rest after the closing --- line.
func splitFrontMatter(data []byte) (fm, body []byte, ok bool) {
	rest, found := bytes.CutPrefix(data, []byte("---\n"))
	if !found {
		return nil, nil, false
	}
	if after, found := bytes.CutPrefix(rest, []byte("---\n")); found {
		return nil, after, true
	}
	i := bytes.Index(rest, []byte("\n---\n"))
	if i < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return rest[:len(rest)-3], nil, true
		}
		return nil, nil, false
	}
	return rest[:i+1], rest[i+5:], true
}

// yamlScalar returns s as a YAML scalar, quoted only when needed.
func yamlScalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		panic(err) // a string always marshals
	}
	return strings.TrimSuffix(string(out), "\n")
}
