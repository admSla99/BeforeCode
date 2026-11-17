package core

import (
	"slices"
	"strings"

	"github.com/oklog/ulid/v2"
)

// Status is what Derive learns about a plan from its Events.
type Status struct {
	State   State
	ShortID string // from the created event
	Author  Person // the plan author, from the created event
	// RequiredApprovals is the number fixed by the submitted event of the
	// open review round; 0 outside a round.
	RequiredApprovals int
	// Approvers are the emails of the valid Approvals in the open review
	// round, in the order they were given.
	Approvers []string
}

// Derive computes a plan's Status from its Events, sorted by ULID, and the
// git object ID of its current plan.md ("" when there is none), following
// ADR 0002 section 4. Events that are not allowed for their author, belong
// to another plan, do not fit the state at their point, or have an unknown
// type are skipped.
func Derive(plan ulid.ULID, events []Event, planBlob string) Status {
	start := slices.IndexFunc(events, func(e Event) bool { return e.Type == Created && e.Plan == plan })
	if start < 0 {
		return Status{State: StateInvalid}
	}
	created := events[start]
	st := Status{State: StateDraft, ShortID: created.ShortID, Author: created.Author}
	isAuthor := func(p Person) bool { return strings.EqualFold(p.Email, created.Author.Email) }

	inRound := false
	var round []Event // approved events of the open review round
	for _, e := range events[start+1:] {
		if e.Plan != plan {
			continue
		}
		switch e.Type {
		case Submitted:
			if isAuthor(e.Author) && !inRound {
				// A missing count means the policy default of 1 (ADR 0002 §4).
				inRound, round, st.RequiredApprovals = true, nil, max(e.RequiredApprovals, 1)
			}
		case Withdrawn:
			if isAuthor(e.Author) && inRound {
				inRound, round, st.RequiredApprovals = false, nil, 0
			}
		case ChangesRequested:
			if !isAuthor(e.Author) && inRound {
				inRound, round, st.RequiredApprovals = false, nil, 0
			}
		case Approved:
			if !isAuthor(e.Author) && e.Agent == "" && inRound {
				round = append(round, e)
			}
		case Closed:
			st.State, st.RequiredApprovals = StateClosed, 0
			return st
		}
	}
	if !inRound {
		return st
	}
	for _, e := range round {
		email := strings.ToLower(e.Author.Email)
		if e.PlanBlob == planBlob && planBlob != "" && !slices.Contains(st.Approvers, email) {
			st.Approvers = append(st.Approvers, email)
		}
	}
	st.State = StateInReview
	if len(st.Approvers) >= st.RequiredApprovals {
		st.State = StateApproved
	}
	return st
}
