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
	// Claims are the valid Claims by Task ID (ADR 0013 section 4): a Task
	// without one is open. They are derived without reading plan.md, so a
	// Task removed from plan.md keeps its Claim.
	Claims map[string]Claim
}

// Claim is the valid claimed Event of a Task: who holds it.
type Claim struct {
	Event  ulid.ULID `json:"event"`
	Holder Person    `json:"holder"`
	Agent  string    `json:"agent,omitempty"` // the harness, when an agent claimed it
}

// Derive computes a plan's Status from its Events, sorted by ULID, and the
// git object ID of its current plan.md ("" when there is none), following
// ADR 0002 section 4 and ADR 0013 section 4. Events that are not allowed for
// their author, belong to another plan, do not fit the state at their point,
// or have an unknown type are skipped. A claimed Event counts after the first
// valid submitted, for a Task nobody holds: of two Claims on one Task the
// earlier ULID wins. An approved plan with a Claim is in-progress.
func Derive(plan ulid.ULID, events []Event, planBlob string) Status {
	st, _ := derive(plan, events, planBlob)
	return st
}

// SkippedEvents returns the IDs of the events Derive skips, in ULID order,
// for bfc doctor (ADR 0002 section 4, rule 5). Without a valid created event
// every event is skipped. An Approval of an older plan.md is not skipped: it
// counts again if plan.md returns to that version.
func SkippedEvents(plan ulid.ULID, events []Event, planBlob string) []ulid.ULID {
	_, skipped := derive(plan, events, planBlob)
	return skipped
}

func derive(plan ulid.ULID, events []Event, planBlob string) (Status, []ulid.ULID) {
	var skipped []ulid.ULID
	skip := func(es ...Event) {
		for _, e := range es {
			skipped = append(skipped, e.ID)
		}
	}
	start := slices.IndexFunc(events, func(e Event) bool { return e.Type == Created && e.Plan == plan })
	if start < 0 {
		skip(events...)
		return Status{State: StateInvalid}, skipped
	}
	skip(events[:start]...)
	created := events[start]
	st := Status{State: StateDraft, ShortID: created.ShortID, Author: created.Author}
	isAuthor := func(p Person) bool { return strings.EqualFold(p.Email, created.Author.Email) }

	inRound, submitted := false, false
	var round []Event // approved events of the open review round
	for i, e := range events[start+1:] {
		valid := false
		if e.Plan == plan {
			switch e.Type {
			case Submitted:
				if valid = isAuthor(e.Author) && !inRound; valid {
					// A missing count means the policy default of 1 (ADR 0002 §4).
					inRound, round, st.RequiredApprovals = true, nil, max(e.RequiredApprovals, 1)
					submitted = true
				}
			case Claimed:
				_, held := st.Claims[e.Task]
				if valid = submitted && taskID.MatchString(e.Task) && !held; valid {
					if st.Claims == nil {
						st.Claims = map[string]Claim{}
					}
					st.Claims[e.Task] = Claim{Event: e.ID, Holder: e.Author, Agent: e.Agent}
				}
			case Withdrawn:
				if valid = isAuthor(e.Author) && inRound; valid {
					inRound, round, st.RequiredApprovals = false, nil, 0
				}
			case ChangesRequested:
				if valid = !isAuthor(e.Author) && inRound; valid {
					inRound, round, st.RequiredApprovals = false, nil, 0
				}
			case Approved:
				if valid = !isAuthor(e.Author) && e.Agent == "" && inRound; valid {
					round = append(round, e)
				}
			case Closed:
				st.State, st.RequiredApprovals = StateClosed, 0
				skip(events[start+2+i:]...)
				return st, skipped
			}
		}
		if !valid {
			skip(e)
		}
	}
	if !inRound {
		return st, skipped
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
		if len(st.Claims) > 0 {
			st.State = StateInProgress
		}
	}
	return st, skipped
}
