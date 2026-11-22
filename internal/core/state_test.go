package core_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

// People in the state tests. ada is the plan author.
var (
	jana  = core.Person{Name: "Jana", Email: "jana@example.com"}
	peter = core.Person{Name: "Peter", Email: "peter@example.com"}
)

const (
	blobV1 = "1111111111111111111111111111111111111111"
	blobV2 = "2222222222222222222222222222222222222222"
)

// ev builds a list of events in ULID order from short descriptions:
//
//	"<type> <who> [blob] [n] [T-n]" with who in {ada, jana, peter, agent:jana}
//
// blob is v1 or v2; n is required_approvals for submitted; T-n is the Task
// of a claimed event.
func ev(t *testing.T, specs ...string) []core.Event {
	t.Helper()
	var out []core.Event
	for i, spec := range specs {
		f := strings.Fields(spec)
		e := core.Event{
			ID:   ulid.MustNew(uint64(1000+i), strings.NewReader(strings.Repeat("\x00", 16))),
			Type: core.EventType(f[0]),
			Plan: planID,
			At:   at.Add(time.Duration(i) * time.Minute),
		}
		who := f[1]
		if rest, ok := strings.CutPrefix(who, "agent:"); ok {
			e.Agent, who = "claude-code", rest
		}
		e.Author = map[string]core.Person{"ada": ada, "jana": jana, "peter": peter, "ADA": {Name: "Ada", Email: "ADA@Example.com"}}[who]
		for _, extra := range f[2:] {
			switch extra {
			case "v1":
				e.PlanBlob = blobV1
			case "v2":
				e.PlanBlob = blobV2
			case "otherplan":
				e.Plan = ulid.MustParse("01JA0000000000000000000000")
			case "T-1", "T-2", "T-9":
				e.Task = extra
			default:
				if _, err := fmt.Sscan(extra, &e.RequiredApprovals); err != nil {
					t.Fatalf("bad event spec %q", spec)
				}
			}
		}
		if e.Type == core.Created {
			e.ShortID = "bfc-5v7x"
		}
		out = append(out, e)
	}
	return out
}

func TestDeriveFollowsTheDataFormat(t *testing.T) {
	cases := []struct {
		name      string
		events    []string
		blob      string
		state     core.State
		approvers []string
	}{
		// Creation.
		{"no events", nil, "", core.StateInvalid, nil},
		{"created", []string{"created ada"}, "", core.StateDraft, nil},
		{"created for another plan", []string{"created ada otherplan"}, "", core.StateInvalid, nil},
		{"events before created are skipped", []string{"submitted ada v1 1", "created ada"}, blobV1, core.StateDraft, nil},

		// submitted.
		{"author submits", []string{"created ada", "submitted ada v1 1"}, blobV1, core.StateInReview, nil},
		{"author email compared without case", []string{"created ada", "submitted ADA v1 1"}, blobV1, core.StateInReview, nil},
		{"non-author cannot submit", []string{"created ada", "submitted jana v1 1"}, blobV1, core.StateDraft, nil},
		{"submit outside draft is skipped", []string{"created ada", "submitted ada v1 2", "submitted ada v1 1", "approved jana v1"}, blobV1, core.StateInReview, []string{"jana@example.com"}},
		{"submitted for another plan", []string{"created ada", "submitted ada v1 1 otherplan"}, blobV1, core.StateDraft, nil},

		// approved.
		{"one approval meets one required", []string{"created ada", "submitted ada v1 1", "approved jana v1"}, blobV1, core.StateApproved, []string{"jana@example.com"}},
		{"one approval of two required", []string{"created ada", "submitted ada v1 2", "approved jana v1"}, blobV1, core.StateInReview, []string{"jana@example.com"}},
		{"two approvers meet two required", []string{"created ada", "submitted ada v1 2", "approved jana v1", "approved peter v1"}, blobV1, core.StateApproved, []string{"jana@example.com", "peter@example.com"}},
		{"same approver counts once", []string{"created ada", "submitted ada v1 2", "approved jana v1", "approved jana v1"}, blobV1, core.StateInReview, []string{"jana@example.com"}},
		{"author cannot approve", []string{"created ada", "submitted ada v1 1", "approved ada v1"}, blobV1, core.StateInReview, nil},
		{"agent cannot approve", []string{"created ada", "submitted ada v1 1", "approved agent:jana v1"}, blobV1, core.StateInReview, nil},
		{"approval in draft is skipped", []string{"created ada", "approved jana v1", "submitted ada v1 1"}, blobV1, core.StateInReview, nil},
		{"stale approval after plan.md changed", []string{"created ada", "submitted ada v1 1", "approved jana v1"}, blobV2, core.StateInReview, nil},
		{"approval of the new version", []string{"created ada", "submitted ada v1 1", "approved jana v1", "approved peter v2"}, blobV2, core.StateApproved, []string{"peter@example.com"}},

		// withdrawn.
		{"author withdraws from review", []string{"created ada", "submitted ada v1 1", "withdrawn ada"}, blobV1, core.StateDraft, nil},
		{"author withdraws an approved plan", []string{"created ada", "submitted ada v1 1", "approved jana v1", "withdrawn ada"}, blobV1, core.StateDraft, nil},
		{"non-author cannot withdraw", []string{"created ada", "submitted ada v1 1", "withdrawn jana"}, blobV1, core.StateInReview, nil},
		{"withdraw in draft is skipped", []string{"created ada", "withdrawn ada"}, "", core.StateDraft, nil},
		{"approval after withdraw is outside any round", []string{"created ada", "submitted ada v1 1", "withdrawn ada", "approved jana v1"}, blobV1, core.StateDraft, nil},
		{"resubmit starts a new round", []string{"created ada", "submitted ada v1 1", "approved jana v1", "withdrawn ada", "submitted ada v1 1"}, blobV1, core.StateInReview, nil},

		// changes-requested.
		{"reviewer requests changes", []string{"created ada", "submitted ada v1 1", "changes-requested jana v1"}, blobV1, core.StateDraft, nil},
		{"changes requested after approval", []string{"created ada", "submitted ada v1 1", "approved jana v1", "changes-requested peter v1"}, blobV1, core.StateDraft, nil},
		{"objection wins whatever the order", []string{"created ada", "submitted ada v1 1", "changes-requested peter v1", "approved jana v1"}, blobV1, core.StateDraft, nil},
		{"author cannot request changes", []string{"created ada", "submitted ada v1 1", "changes-requested ada v1"}, blobV1, core.StateInReview, nil},
		{"request in draft is skipped", []string{"created ada", "changes-requested jana v1"}, "", core.StateDraft, nil},

		// closed.
		{"anyone closes a draft", []string{"created ada", "closed peter"}, "", core.StateClosed, nil},
		{"closed in review", []string{"created ada", "submitted ada v1 1", "closed jana"}, blobV1, core.StateClosed, nil},
		{"closed is final", []string{"created ada", "closed jana", "submitted ada v1 1", "approved peter v1"}, blobV1, core.StateClosed, nil},

		{"closed after approval", []string{"created ada", "submitted ada v1 1", "approved jana v1", "closed peter"}, blobV1, core.StateClosed, nil},
		{"submitted without required_approvals counts as 1", []string{"created ada", "submitted ada v1", "approved jana v1"}, blobV1, core.StateApproved, []string{"jana@example.com"}},

		// Events of another plan.
		{"approval for another plan", []string{"created ada", "submitted ada v1 1", "approved jana v1 otherplan"}, blobV1, core.StateInReview, nil},
		{"withdrawal for another plan", []string{"created ada", "submitted ada v1 1", "withdrawn ada otherplan"}, blobV1, core.StateInReview, nil},
		{"objection for another plan", []string{"created ada", "submitted ada v1 1", "changes-requested jana v1 otherplan"}, blobV1, core.StateInReview, nil},
		{"closing another plan", []string{"created ada", "closed jana otherplan"}, "", core.StateDraft, nil},

		// Other events.
		{"reserved and unknown types are skipped", []string{"created ada", "claimed jana", "renamed ada"}, "", core.StateDraft, nil},
		{"second created is skipped", []string{"created ada", "created jana", "submitted jana v1 1"}, blobV1, core.StateDraft, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := core.Derive(planID, ev(t, c.events...), c.blob)
			if got.State != c.state {
				t.Errorf("state = %q, want %q", got.State, c.state)
			}
			if strings.Join(got.Approvers, ",") != strings.Join(c.approvers, ",") {
				t.Errorf("approvers = %q, want %q", got.Approvers, c.approvers)
			}
		})
	}
}

func TestDeriveReportsAuthorShortIDAndRequiredApprovals(t *testing.T) {
	got := core.Derive(planID, ev(t, "created ada", "submitted ada v1 3"), blobV1)

	if got.Author != ada || got.ShortID != "bfc-5v7x" || got.RequiredApprovals != 3 {
		t.Errorf("Derive = %+v", got)
	}
}

// bfc doctor lists the events that state derivation skips (ADR 0002 section
// 4, rule 5).
func TestSkippedEventsAreThoseDeriveIgnores(t *testing.T) {
	cases := []struct {
		name    string
		events  []string
		blob    string
		skipped []int // positions in events
	}{
		{"none", []string{"created ada", "submitted ada v1 1", "approved jana v1"}, blobV1, nil},
		{"no created: all", []string{"submitted ada v1 1", "approved jana v1"}, blobV1, []int{0, 1}},
		{"before created", []string{"submitted ada v1 1", "created ada"}, blobV1, []int{0}},
		{"second created", []string{"created ada", "created jana"}, "", []int{1}},
		{"not allowed for the author", []string{"created ada", "submitted jana v1 1", "submitted ada v1 1", "approved ada v1", "approved agent:jana v1", "changes-requested ada v1", "withdrawn jana"}, blobV1, []int{1, 3, 4, 5, 6}},
		{"does not fit the state", []string{"created ada", "approved jana v1", "withdrawn ada", "changes-requested jana v1", "submitted ada v1 1", "submitted ada v1 1"}, blobV1, []int{1, 2, 3, 5}},
		{"approval after the round ended", []string{"created ada", "submitted ada v1 1", "changes-requested peter v1", "approved jana v1"}, blobV1, []int{3}},
		{"another plan", []string{"created ada", "closed jana otherplan"}, "", []int{1}},
		{"unknown type", []string{"created ada", "claimed jana"}, "", []int{1}},
		{"after closed", []string{"created ada", "closed jana", "submitted ada v1 1"}, blobV1, []int{2}},
		{"a stale approval is not skipped", []string{"created ada", "submitted ada v1 1", "approved jana v1"}, blobV2, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events := ev(t, c.events...)
			var want []ulid.ULID
			for _, i := range c.skipped {
				want = append(want, events[i].ID)
			}
			got := core.SkippedEvents(planID, events, c.blob)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("SkippedEvents = %v, want %v", got, want)
			}
		})
	}
}

// Issue #108 AC-2, AC-3: Claims after the first valid submitted give Tasks
// a holder, the earlier ULID winning; an approved plan with a Claim is
// in-progress, and editing plan.md returns it to in-review with its Claims.
func TestDeriveClaimsAndInProgress(t *testing.T) {
	approved := []string{"created ada", "submitted ada v1 1", "approved jana v1"}
	cases := []struct {
		name    string
		events  []string
		blob    string
		state   core.State
		holders string   // "T-1=jana@example.com,…" in Task order
		skipped []string // the specs of the skipped events
	}{
		{"approved without Claims", approved, blobV1, core.StateApproved, "", nil},
		{"a Claim makes it in-progress", append(approved, "claimed jana T-1"), blobV1, core.StateInProgress, "T-1=jana@example.com", nil},
		{"an agent may claim", append(approved, "claimed agent:peter T-2"), blobV1, core.StateInProgress, "T-2=peter@example.com", nil},
		{"the earlier Claim wins", append(approved, "claimed peter T-1", "claimed jana T-1", "claimed jana T-2"), blobV1, core.StateInProgress,
			"T-1=peter@example.com,T-2=jana@example.com", []string{"claimed jana T-1"}},
		{"a Claim without a Task is skipped", append(approved, "claimed jana"), blobV1, core.StateApproved, "", []string{"claimed jana"}},
		{"a Claim before any submitted is skipped", []string{"created ada", "claimed jana T-1", "submitted ada v1 1", "approved jana v1"}, blobV1, core.StateApproved, "", []string{"claimed jana T-1"}},
		{"editing plan.md returns it to in-review, Claims stay", append(approved, "claimed jana T-1"), blobV2, core.StateInReview, "T-1=jana@example.com", nil},
		{"approving the new version: in-progress again", append(approved, "claimed jana T-1", "approved peter v2"), blobV2, core.StateInProgress, "T-1=jana@example.com", nil},
		{"Claims survive a withdrawal", append(approved, "claimed jana T-1", "withdrawn ada"), blobV1, core.StateDraft, "T-1=jana@example.com", nil},
		{"a Claim after closed is skipped", append(approved, "closed peter", "claimed jana T-1"), blobV1, core.StateClosed, "", []string{"claimed jana T-1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events := ev(t, c.events...)
			got := core.Derive(planID, events, c.blob)
			if got.State != c.state {
				t.Errorf("state = %q, want %q", got.State, c.state)
			}
			var holders []string
			for _, task := range []string{"T-1", "T-2", "T-9"} {
				if cl, ok := got.Claims[task]; ok {
					holders = append(holders, task+"="+cl.Holder.Email)
				}
			}
			if strings.Join(holders, ",") != c.holders {
				t.Errorf("holders = %v, want %s", holders, c.holders)
			}
			var want []ulid.ULID
			for _, spec := range c.skipped {
				for i, s := range c.events {
					if s == spec {
						want = append(want, events[i].ID)
					}
				}
			}
			if got := core.SkippedEvents(planID, events, c.blob); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("skipped = %v, want %v", got, want)
			}
		})
	}
}

const tasksPlan = "## Context\nx\n## Approach\nx\n## Affected areas\nx\n## Out of scope\nx\n## Tasks\n\n- T-1: Add the client.\n- T-2: Wire the flag.\n- T-3: Remove the old provider.\n\n## Acceptance criteria\n\n- AC-1: works\n"

// Issue #108 AC-1: a Claim is refused unless the plan is approved or
// in-progress, the Task is in the current plan.md and it is open.
func TestCheckClaim(t *testing.T) {
	approved := core.Derive(planID, ev(t, "created ada", "submitted ada v1 1", "approved jana v1"), blobV1)
	inProgress := core.Derive(planID, ev(t, "created ada", "submitted ada v1 1", "approved jana v1", "claimed jana T-1"), blobV1)
	inReview := core.Derive(planID, ev(t, "created ada", "submitted ada v1 1"), blobV1)
	plan := []byte(tasksPlan)
	if err := core.CheckClaim(approved, plan, "T-1"); err != nil {
		t.Errorf("approved, open Task: %v", err)
	}
	if err := core.CheckClaim(inProgress, plan, "T-2"); err != nil {
		t.Errorf("in-progress, open Task: %v", err)
	}
	for name, c := range map[string]struct {
		st   core.Status
		task string
		want string
	}{
		"in review":    {inReview, "T-1", "only an approved or in-progress plan"},
		"no such Task": {approved, "T-9", "T-9 is not a Task of plan.md"},
		"held":         {inProgress, "T-1", "T-1 of bfc-5v7x is held by jana@example.com"},
	} {
		if err := core.CheckClaim(c.st, plan, c.task); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

// Issue #108 AC-4, AC-6: the Tasks of plan.md in plan order with their
// state and holder; the first open one is next.
func TestTaskViews(t *testing.T) {
	st := core.Derive(planID, ev(t, "created ada", "submitted ada v1 1", "approved jana v1", "claimed agent:jana T-1", "claimed peter T-9"), blobV1)
	got := core.TaskViews(st, []byte(tasksPlan))
	if len(got) != 3 || got[0].ID != "T-1" || got[0].State != core.TaskClaimed || got[0].Holder.Email != "jana@example.com" || got[0].Agent != "claude-code" ||
		got[1].State != core.TaskOpen || got[1].Text != "Wire the flag." || got[2].ID != "T-3" {
		t.Errorf("TaskViews = %+v", got)
	}
	if next, ok := core.NextTask(st, []byte(tasksPlan)); !ok || next.ID != "T-2" {
		t.Errorf("NextTask = %+v, %v", next, ok)
	}
}
