package core

import (
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ada  = Person{Name: "Ada Lovelace", Email: "ada@example.com"}
	jana = Person{Name: "Jana Nováková", Email: "Jana@Example.com"}
	ben  = Person{Name: "Ben Bitdiddle", Email: "ben@example.com"}
)

const goodPlan = "## Context\nx\n## Approach\nx\n## Affected areas\nx\n## Out of scope\nx\n## Tasks\n- x\n## Acceptance criteria\n\n- AC-1: works\n"

func status(state State, approvers ...string) Status {
	return Status{State: state, ShortID: "bfc-abcd", Author: ada, RequiredApprovals: 2, Approvers: approvers}
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one containing %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q lacks %q", err, p)
		}
	}
}

func TestCheckSubmit(t *testing.T) {
	if err := CheckSubmit(status(StateDraft), ada, []byte(goodPlan)); err != nil {
		t.Errorf("valid submit: %v", err)
	}
	wantErr(t, CheckSubmit(status(StateDraft), ada, nil), "plan.md", "bfc edit")
	wantErr(t, CheckSubmit(status(StateDraft), ada, []byte("## Context\n")), "missing section", "Acceptance criteria")
	wantErr(t, CheckSubmit(status(StateDraft), jana, []byte(goodPlan)), "plan author", "ada@example.com")
	for _, st := range []State{StateInReview, StateApproved, StateClosed, StateInvalid} {
		wantErr(t, CheckSubmit(status(st), ada, []byte(goodPlan)), "bfc-abcd is "+string(st))
	}
}

func TestCheckApprove(t *testing.T) {
	if err := CheckApprove(status(StateInReview), jana); err != nil {
		t.Errorf("valid approve: %v", err)
	}
	wantErr(t, CheckApprove(status(StateInReview), Person{Email: "ADA@example.com"}), "your own plan")
	wantErr(t, CheckApprove(status(StateInReview, "jana@example.com"), jana), "already approved")
	for _, st := range []State{StateDraft, StateApproved, StateClosed, StateInvalid} {
		wantErr(t, CheckApprove(status(st), jana), "bfc-abcd is "+string(st))
	}
}

func TestCheckRequestChanges(t *testing.T) {
	for _, st := range []State{StateInReview, StateApproved} {
		if err := CheckRequestChanges(status(st), jana, "Too vague."); err != nil {
			t.Errorf("request changes in %s: %v", st, err)
		}
	}
	wantErr(t, CheckRequestChanges(status(StateInReview), ada, "x"), "your own plan", "bfc withdraw")
	wantErr(t, CheckRequestChanges(status(StateInReview), jana, "  \n"), "reason")
	for _, st := range []State{StateDraft, StateClosed, StateInvalid} {
		wantErr(t, CheckRequestChanges(status(st), jana, "x"), "bfc-abcd is "+string(st))
	}
}

func TestEncodeMessage(t *testing.T) {
	at := time.Date(2026, 11, 16, 21, 42, 0, 0, time.FixedZone("CET", 3600))
	got := string(EncodeMessage(Message{Author: jana, At: at, Body: "Too vague.\r\nSplit it."}))
	want := "---\nauthor:\n  name: Jana Nováková\n  email: Jana@Example.com\nat: 2026-11-16T20:42:00Z\n---\n\nToo vague.\nSplit it.\n"
	if got != want {
		t.Errorf("EncodeMessage =\n%s\nwant\n%s", got, want)
	}
}

// The events a review command writes lead Derive to the expected state.
func TestReviewEventsDriveTheState(t *testing.T) {
	plan := ulid.MustParseStrict("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	n := uint64(0)
	ev := func(typ EventType, who Person, blob string) Event {
		n++
		return Event{ID: ulid.MustNew(n, strings.NewReader(strings.Repeat("\x00", 16))), Type: typ, Plan: plan, Author: who, PlanBlob: blob}
	}
	created := ev(Created, ada, "")
	submitted := ev(Submitted, ada, "v1")
	submitted.RequiredApprovals = 2
	events := []Event{created, submitted, ev(Approved, jana, "v1")}
	if st := Derive(plan, events, "v1"); st.State != StateInReview {
		t.Errorf("1 of 2: %s", st.State)
	}
	events = append(events, ev(Approved, ben, "v1"))
	if st := Derive(plan, events, "v1"); st.State != StateApproved {
		t.Errorf("2 of 2: %s", st.State)
	}
	if st := Derive(plan, events, "v2"); st.State != StateInReview || len(st.Approvers) != 0 {
		t.Errorf("after an edit: %s %v", st.State, st.Approvers)
	}
	events = append(events, ev(ChangesRequested, jana, "v1"))
	if st := Derive(plan, events, "v1"); st.State != StateDraft {
		t.Errorf("after changes requested: %s", st.State)
	}
}
