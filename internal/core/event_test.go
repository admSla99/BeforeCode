package core_test

import (
	"strings"
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

// bfc doctor validates every event file against the data format (ADR 0002
// sections 1, 3 and 4).
func TestValidateEventFollowsTheDataFormat(t *testing.T) {
	const name = "01JD3K8Q2X0000000000000000.yaml"
	const common = "plan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A, email: a@example.com}\nat: 2026-11-16T21:30:00Z\n"
	const blob = "plan_blob: 3b18e512dba79e4c8300dd08aeb37f8e728b8dad\n"
	const message = "message: 01JD3K8Q2Y0000000000000000\n"
	valid := map[string]string{
		"created":                    "type: created\n" + common + "short_id: bfc-5v7x\n",
		"created, longer short ID":   "type: created\n" + common + "short_id: bfc-5v7x9y\n",
		"submitted":                  "type: submitted\n" + common + blob + "required_approvals: 2\n",
		"submitted without a count":  "type: submitted\n" + common + blob,
		"withdrawn":                  "type: withdrawn\n" + common,
		"approved":                   "type: approved\n" + common + blob,
		"approved, SHA-256 blob":     "type: approved\n" + common + "plan_blob: " + strings.Repeat("ab", 32) + "\n",
		"changes-requested":          "type: changes-requested\n" + common + blob + message,
		"closed":                     "type: closed\n" + common,
		"closed with a message":      "type: closed\n" + common + message,
		"unknown fields are ignored": "type: withdrawn\n" + common + "future: x\n",
	}
	for what, data := range valid {
		if err := core.ValidateEvent(planID, name, []byte(data)); err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}

	invalid := []struct{ what, name, data, want string }{
		{"name not a ULID", "nope.yaml", "type: withdrawn\n" + common, "file name"},
		{"name in lower case", "01jd3k8q2x0000000000000000.yaml", "type: withdrawn\n" + common, "file name"},
		{"name without .yaml", "01JD3K8Q2X0000000000000000.yml", "type: withdrawn\n" + common, "file name"},
		{"not YAML", name, "type: [", "event"},
		{"no author email", name, "type: withdrawn\nplan: 01JD3K8Q2W5V7X9Y0Z1A2B3C4D\nauthor: {name: A}\nat: 2026-11-16T21:30:00Z\n", "author.email"},
		{"another plan", name, "type: withdrawn\nplan: 01JA0000000000000000000000\nauthor: {name: A, email: a@example.com}\nat: 2026-11-16T21:30:00Z\n", "plan"},
		{"unknown type", name, "type: renamed\n" + common, "unknown type"},
		{"reserved type", name, "type: claimed\n" + common, "reserved"},
		{"created without short_id", name, "type: created\n" + common, "short_id"},
		{"short_id of another ULID", name, "type: created\n" + common + "short_id: bfc-aaaa\n", "short_id"},
		{"short_id too short", name, "type: created\n" + common + "short_id: bfc-5v7\n", "short_id"},
		{"submitted without plan_blob", name, "type: submitted\n" + common, "plan_blob"},
		{"approved with a bad plan_blob", name, "type: approved\n" + common + "plan_blob: xyz\n", "plan_blob"},
		{"changes-requested without message", name, "type: changes-requested\n" + common + blob, "message"},
		{"closed with a bad message", name, "type: closed\n" + common + "message: hello\n", "message"},
	}
	for _, c := range invalid {
		err := core.ValidateEvent(planID, c.name, []byte(c.data))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: ValidateEvent = %v, want an error about %q", c.what, err, c.want)
		}
	}
}
