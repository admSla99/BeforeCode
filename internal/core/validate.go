package core

import (
	"fmt"
	"regexp"
	"strings"
)

// requiredPlanSections are the level-2 headings plan.md must have, matched
// without regard to case (ADR 0002 section 6).
var requiredPlanSections = []string{"Context", "Approach", "Affected areas", "Out of scope", "Tasks", "Acceptance criteria"}

// Problem is something in a document that does not follow the data format.
type Problem struct {
	Line    int // 1-based; 0 when the problem is about the whole file
	Message string
}

func (p Problem) String() string {
	if p.Line == 0 {
		return p.Message
	}
	return fmt.Sprintf("line %d: %s", p.Line, p.Message)
}

// Task is a unit of work in the ## Tasks section of plan.md (ADR 0013
// section 3): its ID (T-1, T-2, …) and its text, continuation lines
// included and joined by newlines.
type Task struct {
	ID   string
	Text string
}

var (
	headingPattern  = regexp.MustCompile(`^##\s+(.*?)\s*$`)
	listItemPattern = regexp.MustCompile(`^(?:[-*+]|[0-9]+[.)])(?:\s|$)`)
)

// itemKind is a section of plan.md whose top-level list items carry stable
// IDs: acceptance criteria and Tasks follow the same rules.
type itemKind struct {
	pattern *regexp.Regexp // the number in group 1, the text in group 2
	prefix  string         // "AC-" or "T-"
	noun    string         // for the problem of a malformed item
}

const (
	outOfScopeSection = "out of scope"
	criteriaSection   = "acceptance criteria"
	tasksSection      = "tasks"
)

// itemSections maps the lower-case heading of a section to its kind.
var itemSections = map[string]itemKind{
	criteriaSection: {regexp.MustCompile(`^- AC-([1-9][0-9]*):(?: (.*))?$`), "AC-", "an acceptance criterion"},
	tasksSection:    {regexp.MustCompile(`^- T-([1-9][0-9]*):(?: (.*))?$`), "T-", "a task"},
}

// ValidatePlan checks plan.md against ADR 0002 section 6 and ADR 0013
// section 3: every required section is present, the Acceptance criteria
// section holds at least one criterion, each a top-level item
// "- AC-<n>: <text>" with a unique ID, and the Tasks section holds only
// items "- T-<n>: <text>" with unique IDs (it may be empty). Continuation
// lines are indented by at least two spaces. HTML comments and fenced code
// blocks are ignored.
func ValidatePlan(plan []byte) []Problem {
	return scanPlan(plan).problems
}

// AcceptanceCriteria returns the IDs of the acceptance criteria of plan.md
// (AC-1, AC-2, …) in the order they appear, each once. List items that are
// not of the form "- AC-<n>: <text>" are left out.
func AcceptanceCriteria(plan []byte) []string {
	var ids []string
	for _, c := range scanPlan(plan).items[criteriaSection] {
		ids = append(ids, c.id)
	}
	return ids
}

// Tasks returns the Tasks of plan.md in the order they appear, each ID
// once. List items that are not of the form "- T-<n>: <text>" are left out.
func Tasks(plan []byte) []Task {
	var tasks []Task
	for _, t := range scanPlan(plan).items[tasksSection] {
		tasks = append(tasks, Task{ID: t.id, Text: t.text})
	}
	return tasks
}

// item is a list item with an ID: an acceptance criterion or a Task.
type item struct {
	id, text string
	line     int
}

// scanned is what scanPlan learns from plan.md.
type scanned struct {
	problems []Problem
	items    map[string][]item // section → its items, the first of each ID
	// text holds each section's lines without HTML comments; a heading
	// that appears twice gets the lines of both.
	text map[string]string
}

// scanPlan returns the problems of plan.md and its items with IDs.
func scanPlan(plan []byte) scanned {
	r := scanned{items: map[string][]item{}, text: map[string]string{}}
	sections := map[string]bool{}
	current := "" // the current section
	section := "" // the current section, when it has items with IDs
	inComment, inFence := false, false
	seen := map[string]int{} // item ID → line
	var cur *item            // the item continuation lines extend
	blanks := 0              // blank lines since the last line of cur
	itemFence := false       // inside a code block indented under cur
	closeItem := func() {
		if cur == nil {
			return
		}
		if cur.text == "" {
			r.problems = append(r.problems, Problem{cur.line, cur.id + " has no text"})
		}
		r.items[section] = append(r.items[section], *cur)
		cur, blanks, itemFence = nil, 0, false
	}
	for i, line := range strings.Split(strings.ReplaceAll(string(plan), "\r\n", "\n"), "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
		isHeading := !inFence && !inComment && headingPattern.MatchString(line)
		isComment := inComment || (!inFence && strings.HasPrefix(trimmed, "<!--"))
		if current != "" && !isHeading && !isComment {
			r.text[current] += line + "\n"
		}
		if cur != nil {
			indented := strings.HasPrefix(line, "  ")
			switch {
			case trimmed == "" && !itemFence:
				blanks++
				continue
			case itemFence || indented:
				if cur.text == "" {
					cur.text = trimmed
				} else {
					cur.text += strings.Repeat("\n", blanks+1) + trimmed
				}
				blanks = 0
				if isFence {
					itemFence = !itemFence
				}
				continue
			}
			closeItem()
		}
		if isFence {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if inComment || strings.HasPrefix(trimmed, "<!--") {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			name := strings.ToLower(m[1])
			sections[name] = true
			current = name
			section = ""
			if _, ok := itemSections[name]; ok {
				section = name
			}
			continue
		}
		if section == "" || !listItemPattern.MatchString(line) {
			continue
		}
		kind := itemSections[section]
		m := kind.pattern.FindStringSubmatch(line)
		if m == nil {
			r.problems = append(r.problems, Problem{n, fmt.Sprintf("not %s: %q; write \"- %s<n>: <text>\"", kind.noun, line, kind.prefix)})
			continue
		}
		id := kind.prefix + m[1]
		if first, dup := seen[id]; dup {
			r.problems = append(r.problems, Problem{n, fmt.Sprintf("%s is used more than once (first on line %d)", id, first)})
			if strings.TrimSpace(m[2]) == "" {
				r.problems = append(r.problems, Problem{n, id + " has no text"})
			}
			continue
		}
		seen[id] = n
		cur = &item{id: id, text: strings.TrimSpace(m[2]), line: n}
	}
	closeItem()
	for _, s := range requiredPlanSections {
		if !sections[strings.ToLower(s)] {
			r.problems = append(r.problems, Problem{0, fmt.Sprintf("missing section \"## %s\"", s)})
		}
	}
	if sections[criteriaSection] && len(r.items[criteriaSection]) == 0 {
		r.problems = append(r.problems, Problem{0, "no acceptance criteria; add \"- AC-1: <text>\""})
	}
	return r
}

// Criterion is an acceptance criterion of plan.md with its text,
// continuation lines included and joined by newlines.
type Criterion struct {
	ID   string
	Text string
}

// Criteria returns the acceptance criteria of plan.md with their text, in
// the order of AcceptanceCriteria.
func Criteria(plan []byte) []Criterion {
	var out []Criterion
	for _, c := range scanPlan(plan).items[criteriaSection] {
		out = append(out, Criterion{ID: c.id, Text: c.text})
	}
	return out
}

// OutOfScope returns the text of the Out of scope section of plan.md,
// trimmed and without HTML comments; "" when there is none.
func OutOfScope(plan []byte) string {
	return strings.TrimSpace(scanPlan(plan).text[outOfScopeSection])
}
