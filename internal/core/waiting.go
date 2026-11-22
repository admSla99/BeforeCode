package core

import (
	"slices"
	"strings"

	"github.com/oklog/ulid/v2"
)

// PlanWork is what WaitingOn needs to know of one plan: its status, its
// Messages in ULID order and the check that counts for each pull request
// (PullRequestChecks).
type PlanWork struct {
	ID       ulid.ULID
	ShortID  string
	Title    string
	Status   Status
	Messages []Message
	Checks   []PullRequestCheck
}

// PlanRef names a plan in a list.
type PlanRef struct {
	ID      ulid.ULID
	ShortID string
	Title   string
}

// WaitingFinding is a Finding on one of the person's plans.
type WaitingFinding struct {
	PlanRef
	Finding Finding
}

// WaitingQuestion is an open Question addressed to the person.
type WaitingQuestion struct {
	PlanRef
	Message Message
}

// Waiting is what waits on one person: the Inbox and Prime's Waiting on
// you. The lists are never nil.
type Waiting struct {
	Findings  []WaitingFinding
	Reviews   []PlanRef
	Questions []WaitingQuestion
}

// Empty reports whether nothing waits.
func (w Waiting) Empty() bool {
	return len(w.Findings) == 0 && len(w.Reviews) == 0 && len(w.Questions) == 0
}

// Waits reports whether something on a plan in state st can wait on
// someone: not when it is final (closed, done) or invalid.
func Waits(st State) bool {
	return st != StateClosed && st != StateDone && st != StateInvalid
}

// WaitingOn returns what waits on p among plans, in their order:
// the Findings that are not acknowledged on the plans p wrote, the plans in
// review p may approve now (no open Question keeps them from it), and the
// open Questions addressed to p. Nothing waits on a plan that is final
// (closed or done) or invalid (see Waits).
func WaitingOn(p Person, plans []PlanWork) Waiting {
	w := Waiting{Findings: []WaitingFinding{}, Reviews: []PlanRef{}, Questions: []WaitingQuestion{}}
	for _, pl := range plans {
		if !Waits(pl.Status.State) {
			continue
		}
		ref := PlanRef{ID: pl.ID, ShortID: pl.ShortID, Title: pl.Title}
		if samePerson(p, pl.Status.Author) {
			for _, f := range Findings(pl.Checks) {
				if !f.Acknowledged {
					w.Findings = append(w.Findings, WaitingFinding{ref, f})
				}
			}
		}
		open := OpenQuestions(pl.Messages)
		if pl.Status.State == StateInReview && CheckApprove(pl.Status, p) == nil && CheckOpenQuestions(pl.Status, open) == nil {
			w.Reviews = append(w.Reviews, ref)
		}
		for _, q := range open {
			if slices.ContainsFunc(q.To, func(to string) bool { return strings.EqualFold(to, p.Email) }) {
				w.Questions = append(w.Questions, WaitingQuestion{ref, q})
			}
		}
	}
	return w
}
