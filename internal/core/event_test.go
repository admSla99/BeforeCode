package core_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/admSla99/BeforeCode/internal/core"
)

var (
	planID  = ulid.MustParse("01JD3K8Q2W5V7X9Y0Z1A2B3C4D")
	eventID = ulid.MustParse("01JD3K8Q2X0000000000000000")
	ada     = core.Person{Name: "Ada Lovelace", Email: "ada@example.com"}
	at      = time.Date(2026, 11, 16, 21, 30, 0, 0, time.UTC)
)

func TestCreatedEventIsWrittenAsInTheDataFormat(t *testing.T) {
	e := core.Event{ID: eventID, Type: core.Created, Plan: planID, Author: ada, At: at, ShortID: "bfc-5v7x"}

	want := `type: created
plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D
author:
  name: Ada Lovelace
  email: ada@example.com
at: 2026-11-16T21:30:00Z
short_id: bfc-5v7x
`
	if got := string(core.EncodeEvent(e)); got != want {
		t.Errorf("EncodeEvent =\n%s\nwant\n%s", got, want)
	}
}

func TestEventsParseBack(t *testing.T) {
	cases := []core.Event{
		{ID: eventID, Type: core.Created, Plan: planID, Author: ada, At: at, ShortID: "bfc-5v7x"},
		{ID: eventID, Type: core.Submitted, Plan: planID, Author: ada, At: at, PlanBlob: "3b18e512dba79e4c8300dd08aeb37f8e728b8dad", RequiredApprovals: 2},
		{ID: eventID, Type: core.Approved, Plan: planID, Author: ada, At: at, Agent: "claude-code", PlanBlob: "3b18e5"},
	}
	for _, e := range cases {
		got, err := core.ParseEvent(eventID, core.EncodeEvent(e))
		if err != nil {
			t.Fatalf("ParseEvent(%s): %v", e.Type, err)
		}
		if got != e {
			t.Errorf("round trip of %+v gave %+v", e, got)
		}
	}
}

func TestParseEventIgnoresUnknownFieldsButNeedsTheCommonOnes(t *testing.T) {
	ok := "type: created\nplan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A, email: a@example.com}\nat: 2026-11-16T21:30:00Z\nshort_id: bfc-5v7x\nfuture_field: x\n"
	if _, err := core.ParseEvent(eventID, []byte(ok)); err != nil {
		t.Errorf("unknown field rejected: %v", err)
	}
	for name, data := range map[string]string{
		"no type":  "plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A, email: a@example.com}\nat: 2026-11-16T21:30:00Z\n",
		"bad plan": "type: created\nplan: nope\nauthor: {name: A, email: a@example.com}\nat: 2026-11-16T21:30:00Z\n",
		"no email": "type: created\nplan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A}\nat: 2026-11-16T21:30:00Z\n",
		"bad time": "type: created\nplan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A, email: a@example.com}\nat: yesterday\n",
		"not yaml": "type: [",
	} {
		if _, err := core.ParseEvent(eventID, []byte(data)); err == nil {
			t.Errorf("%s: ParseEvent succeeded", name)
		}
	}
}

func TestANewPlanIsADraft(t *testing.T) {
	events := []core.Event{{ID: eventID, Type: core.Created, Plan: planID, Author: ada, At: at, ShortID: "bfc-5v7x"}}

	if got := core.Derive(planID, events, "").State; got != core.StateDraft {
		t.Errorf("Derive = %q, want %q", got, core.StateDraft)
	}
}

func TestAPlanWithoutAValidCreatedEventIsInvalid(t *testing.T) {
	other := ulid.MustParse("01JA0000000000000000000000")
	for name, events := range map[string][]core.Event{
		"no events":  nil,
		"wrong plan": {{ID: eventID, Type: core.Created, Plan: other, Author: ada, At: at, ShortID: "bfc-0000"}},
	} {
		if got := core.Derive(planID, events, "").State; got != core.StateInvalid {
			t.Errorf("%s: Derive = %q, want %q", name, got, core.StateInvalid)
		}
	}
}
