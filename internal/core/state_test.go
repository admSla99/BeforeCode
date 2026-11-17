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
//	"<type> <who> [blob] [n]" with who in {ada, jana, peter, agent:jana}
//
// blob is v1 or v2; n is required_approvals for submitted.
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
