package core

import (
	"fmt"
	"regexp"
	"strings"
)

// RequiredPlanSections are the level-2 headings plan.md must have, matched
// without regard to case (ADR 0002 section 6).
var RequiredPlanSections = []string{"Context", "Approach", "Affected areas", "Out of scope", "Tasks", "Acceptance criteria"}

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

var (
	headingPattern   = regexp.MustCompile(`^##\s+(.*?)\s*$`)
	criterionPattern = regexp.MustCompile(`^- AC-([1-9][0-9]*):(.*)$`)
)

// ValidatePlan checks plan.md against ADR 0002 section 6: every required
// section is present, and the Acceptance criteria section holds at least one
// criterion, each a top-level item "- AC-<n>: <text>" with a unique ID.
// HTML comments are ignored.
func ValidatePlan(plan []byte) []Problem {
	var problems []Problem
	sections := map[string]bool{}
	inCriteria, inComment := false, false
	seen := map[string]int{} // criterion ID → line
	for i, line := range strings.Split(strings.ReplaceAll(string(plan), "\r\n", "\n"), "\n") {
		n := i + 1
		if inComment || strings.HasPrefix(strings.TrimSpace(line), "<!--") {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			name := strings.ToLower(m[1])
			sections[name] = true
			inCriteria = name == "acceptance criteria"
			continue
		}
		if !inCriteria || !strings.HasPrefix(line, "- ") && line != "-" {
			continue
		}
		m := criterionPattern.FindStringSubmatch(line)
		switch {
		case m == nil:
			problems = append(problems, Problem{n, fmt.Sprintf("not an acceptance criterion: %q; write \"- AC-<n>: <text>\"", line)})
		case strings.TrimSpace(m[2]) == "":
			problems = append(problems, Problem{n, fmt.Sprintf("AC-%s has no text", m[1])})
			seen["AC-"+m[1]] = n
		default:
			id := "AC-" + m[1]
			if first, dup := seen[id]; dup {
				problems = append(problems, Problem{n, fmt.Sprintf("%s is used more than once (first on line %d)", id, first)})
			}
			seen[id] = n
		}
	}
	for _, s := range RequiredPlanSections {
		if !sections[strings.ToLower(s)] {
			problems = append(problems, Problem{0, fmt.Sprintf("missing section \"## %s\"", s)})
		}
	}
	if sections["acceptance criteria"] && len(seen) == 0 {
		problems = append(problems, Problem{0, "no acceptance criteria; add \"- AC-1: <text>\""})
	}
	return problems
}
