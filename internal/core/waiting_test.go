package core

import (
	"testing"

	"github.com/oklog/ulid/v2"
)

func msgID(s string) ulid.ULID { return ulid.MustParseStrict(s) }

// Issue #106 AC-1: what waits on a person: unacknowledged Findings of the
// newest check of each pull request on their own plans, plans in review
// they may approve now, and open Questions addressed to them.
func TestWaitingOn(t *testing.T) {
	findings := []PullRequestCheck{
		{Check: Check{PR: 7}, Findings: []Finding{{PR: 7, Criterion: "AC-2"}, {PR: 7, OutOfScope: "Touches billing."}}},
		{Check: Check{PR: 9}, Findings: []Finding{{PR: 9, Criterion: "AC-1", Acknowledged: true}}},
	}
	question := func(id string, to ...string) Message {
		return Message{ID: msgID(id), Author: ben, To: to, Body: "Which cards?"}
	}
	plans := []PlanWork{
		// Ada's plan in progress with Findings and a Question to her.
		{ShortID: "bfc-aaaa", Title: "Payments", Status: Status{State: StateApproved, Author: ada},
			Checks: findings, Messages: []Message{question("01JD3K8Q2W5V7X9Y0Z1A2B3C40", "ADA@example.com")}},
		// Jana's plan in review: Ada may approve it.
		{ShortID: "bfc-bbbb", Title: "Refunds", Status: Status{State: StateInReview, Author: jana, RequiredApprovals: 1}},
		// Jana's plan in review with an open Question: Ada may not approve
		// it yet, and the Question is to Jana.
		{ShortID: "bfc-cccc", Title: "Invoices", Status: Status{State: StateInReview, Author: jana, RequiredApprovals: 1},
			Messages: []Message{question("01JD3K8Q2W5V7X9Y0Z1A2B3C41", "jana@example.com")}},
		// Jana's plan with Findings: not Ada's.
		{ShortID: "bfc-dddd", Title: "Exports", Status: Status{State: StateApproved, Author: jana}, Checks: findings},
		// Ada's closed plan: nothing waits.
		{ShortID: "bfc-eeee", Title: "Old", Status: Status{State: StateClosed, Author: ada}, Checks: findings,
			Messages: []Message{question("01JD3K8Q2W5V7X9Y0Z1A2B3C42", "ada@example.com")}},
	}

	w := WaitingOn(ada, plans)

	if len(w.Findings) != 2 || w.Findings[0].ShortID != "bfc-aaaa" || w.Findings[0].Finding.Criterion != "AC-2" ||
		w.Findings[1].Finding.OutOfScope != "Touches billing." || w.Findings[0].Title != "Payments" {
		t.Errorf("findings = %+v", w.Findings)
	}
	if len(w.Reviews) != 1 || w.Reviews[0] != (PlanRef{ShortID: "bfc-bbbb", Title: "Refunds"}) {
		t.Errorf("reviews = %+v", w.Reviews)
	}
	if len(w.Questions) != 1 || w.Questions[0].ShortID != "bfc-aaaa" || w.Questions[0].Message.ID != msgID("01JD3K8Q2W5V7X9Y0Z1A2B3C40") {
		t.Errorf("questions = %+v", w.Questions)
	}
	if w.Empty() {
		t.Errorf("Empty with items")
	}
	if !WaitingOn(ben, nil).Empty() || WaitingOn(ben, nil).Findings == nil {
		t.Errorf("nothing waits: the lists are empty, not nil")
	}
}
