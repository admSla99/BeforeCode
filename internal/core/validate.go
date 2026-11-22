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
	section string         // lower-case heading
	pattern *regexp.Regexp // the number in group 1, the text in group 2
	prefix  string         // "AC-" or "T-"
	noun    string         // for the problem of a malformed item
}

var (
	criteriaKind = itemKind{"acceptance criteria", regexp.MustCompile(`^- AC-([1-9][0-9]*):(?: (.*))?$`), "AC-", "an acceptance criterion"}
	tasksKind    = itemKind{"tasks", regexp.MustCompile(`^- T-([1-9][0-9]*):(?: (.*))?$`), "T-", "a task"}
)

// ValidatePlan checks plan.md against ADR 0002 section 6 and ADR 0013
// section 3: every required section is present, the Acceptance criteria
// section holds at least one criterion, each a top-level item
// "- AC-<n>: <text>" with a unique ID, and the Tasks section holds only
// items "- T-<n>: <text>" with unique IDs (it may be empty). HTML comments
// and fenced code blocks are ignored.
func ValidatePlan(plan []byte) []Problem {
	return scanPlan(plan).problems
}

// AcceptanceCriteria returns the IDs of the acceptance criteria of plan.md
// (AC-1, AC-2, …) in the order they appear, each once. List items that are
// not of the form "- AC-<n>: <text>" are left out.
func AcceptanceCriteria(plan []byte) []string {
	var ids []string
	for _, c := range scanPlan(plan).items[criteriaKind.section] {
		ids = append(ids, c.ID)
	}
	return ids
}

// Tasks returns the Tasks of plan.md in the order they appear, each ID
// once. List items that are not of the form "- T-<n>: <text>" are left out.
func Tasks(plan []byte) []Task {
	return scanPlan(plan).items[tasksKind.section]
}

// scanned is what scanPlan learns from plan.md.
type scanned struct {
	problems []Problem
	items    map[string][]Task // section → its valid items, first of each ID
}

// scanPlan returns the problems of plan.md and its items with IDs.
func scanPlan(plan []byte) scanned {
	r := scanned{items: map[string][]Task{}}
	sections := map[string]bool{}
	var kind *itemKind
	inComment, inFence := false, false
	seen := map[string]int{} // item ID → line
	var open *Task           // the item that continuation lines extend
	var blanks int           // blank lines since the last line of open
	for i, line := range strings.Split(strings.ReplaceAll(string(plan), "\r\n", "\n"), "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence, open = !inFence, nil
			continue
		}
		if inFence {
			continue
		}
		if inComment || strings.HasPrefix(trimmed, "<!--") {
			inComment, open = !strings.Contains(line, "-->"), nil
			continue
		}
		if open != nil {
			switch {
			case trimmed == "":
				blanks++
				continue
			case strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t"):
				open.Text += strings.Repeat("\n", blanks+1) + strings.TrimSpace(line)
				blanks = 0
				continue
			}
			open, blanks = nil, 0
		}
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			name := strings.ToLower(m[1])
			sections[name] = true
			kind = nil
			for _, k := range []*itemKind{&criteriaKind, &tasksKind} {
				if name == k.section {
					kind = k
				}
			}
			continue
		}
		if kind == nil || !listItemPattern.MatchString(line) {
			continue
		}
		m := kind.pattern.FindStringSubmatch(line)
		if m == nil {
			r.problems = append(r.problems, Problem{n, fmt.Sprintf("not %s: %q; write \"- %s<n>: <text>\"", kind.noun, line, kind.prefix)})
			continue
		}
		id := kind.prefix + m[1]
		text := strings.TrimSpace(m[2])
		if first, dup := seen[id]; dup {
			r.problems = append(r.problems, Problem{n, fmt.Sprintf("%s is used more than once (first on line %d)", id, first)})
		} else {
			seen[id] = n
			items := append(r.items[kind.section], Task{ID: id, Text: text})
			r.items[kind.section] = items
			open = &items[len(items)-1]
		}
		if text == "" {
			r.problems = append(r.problems, Problem{n, id + " has no text"})
		}
	}
	for _, s := range requiredPlanSections {
		if !sections[strings.ToLower(s)] {
			r.problems = append(r.problems, Problem{0, fmt.Sprintf("missing section \"## %s\"", s)})
		}
	}
	if sections[criteriaKind.section] && len(r.items[criteriaKind.section]) == 0 {
		r.problems = append(r.problems, Problem{0, "no acceptance criteria; add \"- AC-1: <text>\""})
	}
	return r
}
