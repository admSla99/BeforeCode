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
	// Tasks are the Tasks with a valid Claim, by Task ID (ADR 0013 section
	// 4): a Task that is not here is open. They are derived without reading
	// plan.md, so a Task removed from plan.md keeps its state.
	Tasks map[string]TaskStatus
}

// TaskStatus is the state of a Task that was claimed.
type TaskStatus struct {
	State TaskState `json:"state"` // claimed, paused or completed
	// Claim is the valid Claim of the holder while claimed, and of the last
	// holder once the Task is paused or completed.
	Claim Claim `json:"claim"`
	// Handoff is the ULID of the Handoff of the last released or completed
	// Event, "" while there is none.
	Handoff string `json:"handoff,omitempty"`
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
// valid submitted, for a Task that is open or Paused: of two Claims on one
// Task the earlier ULID wins. A released (the Task is Paused) or completed
// counts when its author holds the Task and it names a Handoff. An approved
// plan with a Claim is in-progress. A done by a person while the plan is
// in-progress makes it done, which is final like closed.
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

// validApprovers returns the emails of the Approvals among round, the
// approved Events of the open review round, that approve planBlob, each
// once, in the order given.
func validApprovers(round []Event, planBlob string) []string {
	var out []string
	for _, e := range round {
		email := strings.ToLower(e.Author.Email)
		if e.PlanBlob == planBlob && planBlob != "" && !slices.Contains(out, email) {
			out = append(out, email)
		}
	}
	return out
}

// HandoffEvents returns, by the ULID of the Handoff it names, each valid
// released or completed Event of the plan: a Handoff that is not in it has
// no Event and changes nothing (ADR 0013 section 5).
func HandoffEvents(plan ulid.ULID, events []Event, planBlob string) map[ulid.ULID]ulid.ULID {
	skipped := map[ulid.ULID]bool{}
	for _, id := range SkippedEvents(plan, events, planBlob) {
		skipped[id] = true
	}
	out := map[ulid.ULID]ulid.ULID{}
	for _, e := range events {
		if (e.Type == Released || e.Type == Completed) && !skipped[e.ID] {
			out[ulid.MustParseStrict(e.Handoff)] = e.ID // derive accepts only a ULID
		}
	}
	return out
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
				ts := st.Tasks[e.Task]
				free := ts.State == "" || ts.State == TaskPaused
				if valid = submitted && taskID.MatchString(e.Task) && free; valid {
					if st.Tasks == nil {
						st.Tasks = map[string]TaskStatus{}
					}
					st.Tasks[e.Task] = TaskStatus{State: TaskClaimed, Claim: Claim{Event: e.ID, Holder: e.Author, Agent: e.Agent}, Handoff: ts.Handoff}
				}
			case Released, Completed:
				ts := st.Tasks[e.Task]
				_, err := ulid.ParseStrict(e.Handoff)
				if valid = ts.State == TaskClaimed && samePerson(e.Author, ts.Claim.Holder) && err == nil; valid {
					ts.State, ts.Handoff = TaskPaused, e.Handoff
					if e.Type == Completed {
						ts.State = TaskCompleted
					}
					st.Tasks[e.Task] = ts
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
			case Done:
				// Valid only while the plan is in-progress: approved, with a
				// Claim (ADR 0013 section 4). The Approvals count for the
				// plan.md the done names, not the current one, so a plan.md
				// changed elsewhere before the done arrived does not undo it.
				inProgress := inRound && len(st.Tasks) > 0 && len(validApprovers(round, e.PlanBlob)) >= st.RequiredApprovals
				if valid = e.Agent == "" && inProgress; valid {
					st.State, st.RequiredApprovals, st.Approvers = StateDone, 0, nil
					skip(events[start+2+i:]...)
					return st, skipped
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
	st.Approvers = validApprovers(round, planBlob)
	st.State = StateInReview
	if len(st.Approvers) >= st.RequiredApprovals {
		st.State = StateApproved
		if len(st.Tasks) > 0 {
			st.State = StateInProgress
		}
	}
	return st, skipped
}
